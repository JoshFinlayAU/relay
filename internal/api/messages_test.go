package api

import (
	"context"
	"io"
	"net/http"
	"strings"
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

func TestGetAttachment(t *testing.T) {
	ts := newTestServer(t)
	ctx := context.Background()
	did := uuid.MustParse(createDomainForTest(t, ts.URL, testToken, "att.example"))

	raw := []byte("From: a@ext.example\r\nTo: support@att.example\r\nSubject: Files\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=B\r\n\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\nsee attached\r\n" +
		"--B\r\nContent-Type: text/csv\r\nContent-Disposition: attachment; filename=\"data.csv\"\r\n\r\n" +
		"a,b,c\r\n1,2,3\r\n--B--\r\n")
	ref, err := testBlobs.Put(raw)
	if err != nil {
		t.Fatal(err)
	}
	mid := uuid.New()
	if _, err := testStore.InsertMessage(ctx, store.InsertMessageParams{
		ID: mid, Direction: "inbound", DomainID: &did,
		RcptTo: []string{"support@att.example"}, Status: "received", BodyRef: &ref,
	}); err != nil {
		t.Fatal(err)
	}

	// Body endpoint returns a signed download URL for the attachment.
	_, body := do(t, "GET", ts.URL+"/v1/messages/"+mid.String()+"/body", testToken, nil)
	atts := body["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("attachments = %d, want 1", len(atts))
	}
	url, _ := atts[0].(map[string]any)["url"].(string)
	if url == "" {
		t.Fatal("no signed url returned")
	}
	// Turn the absolute signed URL into a path+query against the test server.
	q := url[strings.Index(url, "/v1/"):]

	// Signed URL fetches with no bearer token.
	resp, err := http.Get(ts.URL + q)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signed fetch = %d, want 200", resp.StatusCode)
	}
	data, _ := io.ReadAll(resp.Body)
	if string(data) != "a,b,c\r\n1,2,3\r\n" && string(data) != "a,b,c\r\n1,2,3" {
		t.Errorf("attachment bytes = %q", data)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "data.csv") {
		t.Errorf("content-disposition = %q", cd)
	}

	getStatus := func(u string) int {
		resp, err := http.Get(u) //nolint:gosec // test URL
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// Tampered signature → 401.
	if getStatus(ts.URL+strings.Replace(q, "sig=", "sig=deadbeef", 1)) != http.StatusUnauthorized {
		t.Errorf("tampered sig should be 401")
	}
	// Bearer token also works (no signature).
	if st, _ := do(t, "GET", ts.URL+"/v1/messages/"+mid.String()+"/attachments/0", testToken, nil); st != http.StatusOK {
		t.Errorf("bearer fetch = %d, want 200", st)
	}
	// No auth at all → 401.
	if getStatus(ts.URL+"/v1/messages/"+mid.String()+"/attachments/0") != http.StatusUnauthorized {
		t.Errorf("unauth fetch should be 401")
	}
}
