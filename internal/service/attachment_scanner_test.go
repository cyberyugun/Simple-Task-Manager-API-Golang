package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-simple-task-api/internal/model"
)

func TestNewAttachmentScannerRequiresConfiguredEndpointWhenRequired(t *testing.T) {
	_, err := NewAttachmentScanner(AttachmentConfig{ScannerRequired: true})
	if err == nil || !strings.Contains(err.Error(), "ATTACHMENT_SCANNER_URL") {
		t.Fatalf("error = %v", err)
	}
}

func TestNewAttachmentScannerRejectsInsecureEndpointByDefault(t *testing.T) {
	_, err := NewAttachmentScanner(AttachmentConfig{ScannerURL: "http://scanner.example.test/scan"})
	if err != ErrAttachmentScannerConfiguration {
		t.Fatalf("error = %v, want %v", err, ErrAttachmentScannerConfiguration)
	}
}

func TestHTTPAttachmentScannerAuthenticatedCleanResponse(t *testing.T) {
	const bearer = "scanner-token"
	const signingSecret = "scanner-signing-secret-32-bytes-long"
	var received attachmentScanRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+bearer {
			t.Fatalf("authorization = %q", got)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &received); err != nil {
			t.Fatal(err)
		}
		timestamp := r.Header.Get("X-Attachment-Scan-Timestamp")
		signature := r.Header.Get("X-Attachment-Scan-Signature")
		if timestamp == "" || signature == "" {
			t.Fatalf("missing scanner signature headers")
		}
		mac := hmac.New(sha256.New, []byte(signingSecret))
		_, _ = mac.Write([]byte(timestamp))
		_, _ = mac.Write([]byte("\n"))
		_, _ = mac.Write(raw)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(signature), []byte(expected)) {
			t.Fatalf("signature = %q, want %q", signature, expected)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"clean":true,"engine":"clamav-gateway","message":"OK"}`)
	}))
	defer server.Close()

	scanner, err := NewAttachmentScanner(AttachmentConfig{
		ScannerURL: server.URL, ScannerBearerToken: bearer,
		ScannerSigningSecret: signingSecret, AllowInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.Scan(t.Context(), model.Attachment{
		ID: 7, WorkspaceID: 9, Provider: model.AttachmentProviderS3Compatible,
		Bucket: "attachments", ObjectKey: "workspaces/9/abc/report.pdf",
		FileName: "report.pdf", ContentType: "application/pdf", SizeBytes: 1234,
		SHA256: strings.Repeat("a", 64), Encryption: "AES256", EncryptionKeyID: "key-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Clean || result.Engine != "clamav-gateway" || result.Message != "OK" {
		t.Fatalf("result = %+v", result)
	}
	if received.AttachmentID != 7 || received.WorkspaceID != 9 || received.ObjectKey == "" ||
		received.SHA256 != strings.Repeat("a", 64) || received.EncryptionKeyID != "key-1" {
		t.Fatalf("received = %+v", received)
	}
}

func TestHTTPAttachmentScannerInfectedAndFailureAreFailClosed(t *testing.T) {
	t.Run("infected", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"clean":false,"engine":"clamav","message":"Eicar-Test-Signature"}`)
		}))
		defer server.Close()

		scanner, err := NewAttachmentScanner(AttachmentConfig{ScannerURL: server.URL, AllowInsecure: true})
		if err != nil {
			t.Fatal(err)
		}
		result, err := scanner.Scan(t.Context(), model.Attachment{ID: 1})
		if err != nil {
			t.Fatal(err)
		}
		if result.Clean || result.Engine != "clamav" || result.Message == "" {
			t.Fatalf("result = %+v", result)
		}
	})

	t.Run("gateway failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		scanner, err := NewAttachmentScanner(AttachmentConfig{ScannerURL: server.URL, AllowInsecure: true})
		if err != nil {
			t.Fatal(err)
		}
		result, err := scanner.Scan(t.Context(), model.Attachment{ID: 1})
		if err == nil {
			t.Fatal("expected scanner error")
		}
		if result.Engine != "remote_gateway" || !strings.Contains(result.Message, "503") {
			t.Fatalf("result = %+v err=%v", result, err)
		}
	})
}

func TestHTTPAttachmentScannerDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("scanner must not follow redirects")
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	scanner, err := NewAttachmentScanner(AttachmentConfig{
		ScannerURL: server.URL, AllowInsecure: true, ScannerTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = scanner.Scan(t.Context(), model.Attachment{ID: 1})
	if err == nil {
		t.Fatal("expected redirect to be rejected")
	}
}
