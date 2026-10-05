package health

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// scrubCases is shared by the unit table and the handler tests: every input
// carries a secret fragment and a host that must survive.
var scrubCases = []struct {
	name   string
	in     string
	secret []string // none may appear in the output
	keep   []string // all must appear in the output
}{
	{"amqp plain", "dial amqp://guest:S3cr3t@broker.internal:5672/vh failed", []string{"S3cr3t", "guest"}, []string{"amqp://<redacted>@broker.internal:5672/vh", "dial", "failed"}},
	{"amqps", "amqps://u:S3cr3t@mq.example.com:5671", []string{"S3cr3t"}, []string{"amqps://<redacted>@mq.example.com:5671"}},
	{"mongodb", "mongodb://root:S3cr3t@mongo-0.mongo:27017,mongo-1.mongo:27017/db?replicaSet=rs0", []string{"S3cr3t", "root"}, []string{"mongo-0.mongo:27017", "mongo-1.mongo:27017", "replicaSet=rs0"}},
	{"mongodb+srv", "mongodb+srv://app:S3cr3t@cluster0.abc.mongodb.net/test", []string{"S3cr3t"}, []string{"mongodb+srv://<redacted>@cluster0.abc.mongodb.net/test"}},
	{"redis", "redis://:S3cr3t@cache.internal:6379/0", []string{"S3cr3t"}, []string{"redis://<redacted>@cache.internal:6379/0"}},
	{"rediss", "rediss://default:S3cr3t@cache.internal:6380", []string{"S3cr3t"}, []string{"rediss://<redacted>@cache.internal:6380"}},
	{"postgres", "postgres://pg:S3cr3t@db.internal:5432/app?sslmode=disable", []string{"S3cr3t"}, []string{"db.internal:5432/app?sslmode=disable"}},
	{"https userinfo", "GET https://bot:S3cr3t@api.example.com/v1 returned 500", []string{"S3cr3t"}, []string{"https://<redacted>@api.example.com/v1", "returned 500"}},
	{"http userinfo", "http://bot:S3cr3t@api.example.com", []string{"S3cr3t"}, []string{"api.example.com"}},
	{"uppercase scheme", "AMQP://guest:S3cr3t@broker.internal", []string{"S3cr3t"}, []string{"AMQP://<redacted>@broker.internal"}},
	{"password contains ://", "amqp://u:p4ss://S3cr3t@broker.internal:5672", []string{"S3cr3t", "p4ss", "u:"}, []string{"amqp://<redacted>@broker.internal:5672"}},
	{"password contains @", "amqp://u:S3cr3t@x@broker.internal:5672", []string{"S3cr3t", "x@"}, []string{"amqp://<redacted>@broker.internal:5672"}},
	{"password contains @ and ://", "amqp://u:S3cr3t@q://z@broker.internal", []string{"S3cr3t", "q://z"}, []string{"amqp://<redacted>@broker.internal"}},
	{"password contains space and quotes", `amqp://u:S3 "cr'3t@broker.internal:5672`, []string{"S3", "cr'3t"}, []string{"amqp://<redacted>@broker.internal:5672"}},
	{"password contains <>", "amqp://u:S3<cr>3t@broker.internal", []string{"S3<cr>", "3t@"}, []string{"amqp://<redacted>@broker.internal"}},
	{"password contains slash and colon", "postgres://u:S3/cr:3t@db.internal/app", []string{"S3/cr", "cr:3t"}, []string{"postgres://<redacted>@db.internal/app"}},
	{"two URIs", "primary amqp://a:S3cr3t@h1.internal:5672 fallback amqps://b:Pw0rd@h2.internal:5671", []string{"S3cr3t", "Pw0rd"}, []string{"amqp://<redacted>@h1.internal:5672", "amqps://<redacted>@h2.internal:5671", "primary", "fallback"}},
	{"two URIs quoted", `"amqp://a:S3cr3t@h1.internal","mongodb://b:Pw0rd@h2.internal"`, []string{"S3cr3t", "Pw0rd"}, []string{"h1.internal", "h2.internal"}},
	{"password= param", "connect failed host=db.internal password=S3cr3t&sslmode=require", []string{"S3cr3t"}, []string{"host=db.internal", "password=<redacted>&sslmode=require"}},
	{"password= with a:b@c", "password=a:b@c0nfid3ntial", []string{"a:b", "c0nfid3ntial"}, []string{"password=<redacted>"}},
	{"password= ends at quote", `dsn "password=S3cr3t" host`, []string{"S3cr3t"}, []string{`"password=<redacted>"`, "host"}},
	{"password= ends at newline", "password=S3cr3t\nnext line", []string{"S3cr3t"}, []string{"password=<redacted>\nnext line"}},
	{"password= with spaces", "password=S3 cr3t pa ss", []string{"S3", "cr3t", "pa ss"}, []string{"password=<redacted>"}},
	{"pass=", "pass=S3cr3t&x=1", []string{"S3cr3t"}, []string{"pass=<redacted>&x=1"}},
	{"passwd=", "passwd=S3cr3t", []string{"S3cr3t"}, []string{"passwd=<redacted>"}},
	{"pwd=", "pwd=S3cr3t", []string{"S3cr3t"}, []string{"pwd=<redacted>"}},
	{"secret=", "client secret=S3cr3t&id=1", []string{"S3cr3t"}, []string{"secret=<redacted>&id=1"}},
	{"token=", "token=S3cr3t", []string{"S3cr3t"}, []string{"token=<redacted>"}},
	{"param mixed case", "PassWord=S3cr3t", []string{"S3cr3t"}, []string{"PassWord=<redacted>"}},
	{"param in URI query", "amqp://u:S3cr3t@h.internal/?token=T0k3n&heartbeat=10", []string{"S3cr3t", "T0k3n"}, []string{"h.internal", "heartbeat=10"}},
	{"param then URI", "password=P4ss://x@y&url=amqp://u:S3cr3t@broker.internal", []string{"P4ss", "S3cr3t"}, []string{"broker.internal"}},
	{"scheme-less", "dial svc: root:S3cr3t@db.internal:5432 refused", []string{"S3cr3t", "root"}, []string{"<redacted>@db.internal:5432", "refused"}},
	{"scheme-less password with colon and @", "root:a:S3cr3t@T41l@db.internal:5432", []string{"S3cr3t", "root", "T41l"}, []string{"@db.internal:5432"}},
	{"scheme-less in quotes", `err="root:S3cr3t@db.internal"`, []string{"S3cr3t", "root"}, []string{"@db.internal"}},
	{"URI in JSON-ish error", `{"error":"dial amqp://u:S3cr3t@broker.internal:5672: refused"}`, []string{"S3cr3t"}, []string{"broker.internal:5672", "refused"}},
}

func TestScrubCredentials(t *testing.T) {
	for _, tc := range scrubCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ScrubCredentials(tc.in)
			for _, s := range tc.secret {
				if strings.Contains(got, s) {
					t.Errorf("leaked %q: %q -> %q", s, tc.in, got)
				}
			}
			for _, k := range tc.keep {
				if !strings.Contains(got, k) {
					t.Errorf("lost %q: %q -> %q", k, tc.in, got)
				}
			}
			if again := ScrubCredentials(got); again != got {
				t.Errorf("not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestScrubLeavesInnocentTextAlone(t *testing.T) {
	for _, in := range []string{
		"", "OK", "mongodb: last success 3s ago", "dial tcp 10.0.0.1:5672: connect: refused",
		"https://api.example.com/v1/status", "amqp://broker.internal:5672", "contact ops@example.com", "12:30 started",
	} {
		if got := ScrubCredentials(in); got != in {
			t.Errorf("changed %q -> %q", in, got)
		}
	}
}

// hostile registers every path a string can take into a body, each carrying a
// URI whose password is "S3cr3t"+"://"+"p4ss", plus a key carrying one.
func hostile(t *testing.T, opts ...RegistryOption) *Registry {
	t.Helper()
	uri := "amqp://u:S3cr3t://p4ss@broker.internal:5672/vh"
	r := New(append([]RegistryOption{WithJitter(1)}, opts...)...)
	r.RegisterReadiness("ready-fail", fixed(Failed, "dial "+uri), WithInterval(10*time.Second))
	r.RegisterReadiness("ready-ok", fixed(OK, "connected to "+uri), WithInterval(10*time.Second))
	r.RegisterReadiness("ready-degraded", fixed(Degraded, "password=a:b@c via "+uri), WithInterval(10*time.Second))
	r.RegisterReadiness("info "+uri, fixed(Failed, "info "+uri), WithInterval(10*time.Second), WithKind(Informational))
	r.RegisterLiveness("facts", FromFacts(func() (Level, string) { return Degraded, "facts " + uri }), WithInterval(10*time.Second))
	wd := NewWatchdog("wd", time.Minute, time.Hour, func() bool { return true })
	r.RegisterLiveness("wd", wd.Liveness(), WithInterval(10*time.Second))
	r.RegisterReadiness("wd", wd.Readiness(), WithInterval(10*time.Second), WithKind(Informational))
	end := wd.Begin("sync " + uri)
	t.Cleanup(end)
	r.MarkWired()
	return r
}

func bodies(t *testing.T, r *Registry) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, h := range map[string]http.HandlerFunc{"live": r.LiveHandler(), "ready": r.ReadyHandler(), "startup": r.StartupHandler()} {
		rec := httpGet(h)
		out[name] = rec
	}
	return out
}

func TestHandlersScrubEveryBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := hostile(t)
		stop := run(r)
		defer stop()
		time.Sleep(15 * time.Second)
		synctest.Wait()
		for scope, body := range bodies(t, r) {
			for _, frag := range []string{"S3cr3t", "p4ss", "a:b@c", "u:S3"} {
				if strings.Contains(body, frag) {
					t.Errorf("%s body leaks %q:\n%s", scope, frag, body)
				}
			}
			if scope != "startup" && !strings.Contains(body, "broker.internal:5672") {
				t.Errorf("%s body lost the host:\n%s", scope, body)
			}
			if scope == "ready" && !strings.Contains(body, "<redacted>") {
				t.Errorf("ready body not redacted (or HTML-escaped):\n%s", body)
			}
		}
	})
}

func TestScrubCoversKeysNotesAndBeforeFirstEvaluation(t *testing.T) {
	// Not yet evaluated and not started: the "not evaluated"/"starting" paths
	// and a credential-bearing check name must still be scrubbed.
	r := New()
	r.RegisterReadiness("db amqp://u:S3cr3t@h.internal", fixed(OK, ""), WithInterval(10*time.Second))
	for scope, body := range bodies(t, r) {
		if strings.Contains(body, "S3cr3t") {
			t.Errorf("%s leaks: %s", scope, body)
		}
	}
	if b := bodies(t, r)["ready"]; !strings.Contains(b, "h.internal") {
		t.Errorf("host lost: %s", b)
	}
}

func TestKeyCollisionAfterScrubKeepsBothChecks(t *testing.T) {
	r := New()
	r.RegisterReadiness("x amqp://a:one@h", fixed(OK, ""), WithInterval(10*time.Second))
	r.RegisterReadiness("x amqp://b:two@h", fixed(OK, ""), WithInterval(10*time.Second))
	_, _, body := call(r.ReadyHandler())
	n := 0
	for k := range body.Checks {
		if strings.HasPrefix(k, "x amqp://<redacted>@h") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("want both colliding checks reported, got %v", body.Checks)
	}
}

func TestWithScrubberRunsAfterBuiltInAndCannotDisableIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var seen []string
		r := New(WithJitter(1), WithScrubber(func(s string) string {
			seen = append(seen, s)
			return strings.ReplaceAll(s, "INTERNAL-9", "<x>")
		}), WithScrubber(nil), WithScrubber(func(s string) string { return s }))
		r.RegisterReadiness("db", fixed(Failed, "amqp://u:S3cr3t@h.internal INTERNAL-9"), WithInterval(10*time.Second))
		r.MarkWired()
		stop := run(r)
		defer stop()
		time.Sleep(15 * time.Second)
		synctest.Wait()
		body := bodies(t, r)["ready"]
		if strings.Contains(body, "S3cr3t") || strings.Contains(body, "INTERNAL-9") || !strings.Contains(body, "h.internal") {
			t.Fatalf("body: %s", body)
		}
		for _, s := range seen {
			if strings.Contains(s, "S3cr3t") {
				t.Fatalf("custom scrubber saw an unscrubbed value: %q", s)
			}
		}
	})
}

func TestPanickingScrubberFailsClosed(t *testing.T) {
	r := New(WithScrubber(func(string) string { panic("boom") }))
	r.RegisterReadiness("db", fixed(OK, ""), WithInterval(10*time.Second))
	code, _, body := call(r.ReadyHandler())
	if code == 0 || strings.Contains(strings.Join(mapVals(body.Checks), ""), "starting") {
		t.Fatalf("unexpected: %d %+v", code, body)
	}
	for _, v := range body.Checks {
		if v != "<redacted>" {
			t.Fatalf("panicking scrubber must fail closed, got %q", v)
		}
	}
}

func mapVals(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func httpGet(h http.HandlerFunc) string {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Body.String()
}

func TestRenderScrubsNote(t *testing.T) {
	r := New()
	b := r.render(Snapshot{Note: "see amqp://u:S3cr3t@h.internal"})
	if strings.Contains(b.Note, "S3cr3t") || !strings.Contains(b.Note, "h.internal") {
		t.Fatalf("note: %q", b.Note)
	}
}
