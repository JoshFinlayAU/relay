package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"relay/internal/store"
)

// An inbound message's detail must surface its webhook delivery history and the
// SPF/DKIM results — not just the (outbound-only) delivery attempts.
func TestGetMessageInboundWebhookDeliveries(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()

	did := uuid.MustParse(createDomainForTest(t, ts.URL, testToken, "inbound-detail.example"))
	mb, err := testStore.CreateMailbox(ctx, store.CreateMailboxParams{
		DomainID: did, LocalPart: "support", WebhookUrl: "https://app.example/inbound", WebhookSecretEnc: []byte("x"),
	})
	if err != nil {
		t.Fatal(err)
	}

	pass := "pass"
	mid := uuid.New()
	if _, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
		ID: mid, Direction: "inbound", DomainID: &did,
		MailFrom: strPtr("a@ext.example"), HeaderFrom: strPtr("a@ext.example"),
		RcptTo: []string{"support@inbound-detail.example"}, Status: "received",
		SpfResult: &pass, DkimResult: &pass,
	}); err != nil {
		t.Fatal(err)
	}
	wd, err := testStore.CreateWebhookDelivery(ctx, store.CreateWebhookDeliveryParams{MailboxID: mb.ID, MessageID: mid})
	if err != nil {
		t.Fatal(err)
	}
	if err := testStore.MarkWebhookSuccess(ctx, store.MarkWebhookSuccessParams{ID: wd.ID, StatusCode: i32ptr(200), ResponseSnippet: strPtr("ok")}); err != nil {
		t.Fatal(err)
	}

	status, out := do(t, "GET", ts.URL+"/v1/messages/"+mid.String(), testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("get message = %d (%v)", status, out)
	}
	msg := out["message"].(map[string]any)
	if msg["spf_result"] != "pass" || msg["dkim_result"] != "pass" {
		t.Errorf("spf/dkim results not surfaced: spf=%v dkim=%v", msg["spf_result"], msg["dkim_result"])
	}
	whs, ok := out["webhook_deliveries"].([]any)
	if !ok || len(whs) != 1 {
		t.Fatalf("expected 1 webhook delivery, got %v", out["webhook_deliveries"])
	}
	first := whs[0].(map[string]any)
	if first["result"] != "success" || first["status_code"].(float64) != 200 {
		t.Errorf("webhook delivery not reported correctly: %v", first)
	}
}

func i32ptr(n int32) *int32 { return &n }

func TestGetMessageBody(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	did := uuid.MustParse(createDomainForTest(t, ts.URL, testToken, "body.example"))

	raw := []byte("From: a@ext.example\r\nTo: support@body.example\r\nSubject: Hi\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=B\r\n\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\nhello plain body\r\n" +
		"--B\r\nContent-Type: text/html\r\n\r\n<p>hello <b>html</b></p>\r\n--B--\r\n")
	ref, err := testBlobs.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	mid := uuid.New()
	if _, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
		ID: mid, Direction: "inbound", DomainID: &did,
		RcptTo: []string{"support@body.example"}, Status: "received", BodyRef: &ref,
	}); err != nil {
		t.Fatal(err)
	}

	status, out := do(t, "GET", ts.URL+"/v1/messages/"+mid.String()+"/body", testToken, nil)
	if status != http.StatusOK {
		t.Fatalf("body = %d (%v)", status, out)
	}
	if txt, _ := out["text"].(string); txt != "hello plain body\r\n" && txt != "hello plain body" {
		t.Errorf("text body = %q", out["text"])
	}
	if html, _ := out["html"].(string); html == "" {
		t.Errorf("expected html body, got %q", out["html"])
	}

	// No body stored → 404.
	mid2 := uuid.New()
	_, _ = testStore.InsertMessage(ctx, store.InsertMessageParams{ID: mid2, Direction: "inbound", DomainID: &did, RcptTo: []string{"x@body.example"}, Status: "received"})
	if st, _ := do(t, "GET", ts.URL+"/v1/messages/"+mid2.String()+"/body", testToken, nil); st != http.StatusNotFound {
		t.Errorf("no-body message = %d, want 404", st)
	}
}
