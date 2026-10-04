package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go-simple-task-api/internal/model"
)

var (
	ErrAttachmentScannerConfiguration = errors.New("invalid attachment scanner configuration")
	ErrAttachmentScannerUnavailable   = errors.New("attachment scanner unavailable")
	ErrAttachmentScannerResponse      = errors.New("invalid attachment scanner response")
)

type HTTPAttachmentScanner struct {
	endpoint      *url.URL
	bearerToken   string
	signingSecret []byte
	client        *http.Client
}

type attachmentScanRequest struct {
	AttachmentID    int64  `json:"attachment_id"`
	WorkspaceID     int64  `json:"workspace_id"`
	Provider        string `json:"provider"`
	Bucket          string `json:"bucket"`
	ObjectKey       string `json:"object_key"`
	FileName        string `json:"file_name"`
	ContentType     string `json:"content_type"`
	SizeBytes       int64  `json:"size_bytes"`
	SHA256          string `json:"sha256"`
	Encryption      string `json:"encryption,omitempty"`
	EncryptionKeyID string `json:"encryption_key_id,omitempty"`
}

func NewAttachmentScanner(cfg AttachmentConfig) (AttachmentScanner, error) {
	rawURL := strings.TrimSpace(cfg.ScannerURL)
	if rawURL == "" {
		if cfg.ScannerRequired {
			return nil, fmt.Errorf("%w: ATTACHMENT_SCANNER_URL is required", ErrAttachmentScannerConfiguration)
		}
		return NoopAttachmentScanner{}, nil
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, ErrAttachmentScannerConfiguration
	}
	if endpoint.Scheme != "https" && !(cfg.AllowInsecure && endpoint.Scheme == "http") {
		return nil, ErrAttachmentScannerConfiguration
	}
	timeout := cfg.ScannerTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &HTTPAttachmentScanner{
		endpoint: endpoint, bearerToken: strings.TrimSpace(cfg.ScannerBearerToken),
		signingSecret: []byte(strings.TrimSpace(cfg.ScannerSigningSecret)), client: client,
	}, nil
}

func (s *HTTPAttachmentScanner) Scan(ctx context.Context, attachment model.Attachment) (model.AttachmentScanResult, error) {
	payload, err := json.Marshal(attachmentScanRequest{
		AttachmentID: attachment.ID, WorkspaceID: attachment.WorkspaceID,
		Provider: attachment.Provider, Bucket: attachment.Bucket, ObjectKey: attachment.ObjectKey,
		FileName: attachment.FileName, ContentType: attachment.ContentType, SizeBytes: attachment.SizeBytes,
		SHA256: attachment.SHA256, Encryption: attachment.Encryption, EncryptionKeyID: attachment.EncryptionKeyID,
	})
	if err != nil {
		return model.AttachmentScanResult{Engine: "remote_gateway"}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return model.AttachmentScanResult{Engine: "remote_gateway"}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-attachment-scanner/1")
	if s.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.bearerToken)
	}
	if len(s.signingSecret) > 0 {
		timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		mac := hmac.New(sha256.New, s.signingSecret)
		_, _ = mac.Write([]byte(timestamp))
		_, _ = mac.Write([]byte("\n"))
		_, _ = mac.Write(payload)
		req.Header.Set("X-Attachment-Scan-Timestamp", timestamp)
		req.Header.Set("X-Attachment-Scan-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return model.AttachmentScanResult{Engine: "remote_gateway"}, fmt.Errorf("%w: %v", ErrAttachmentScannerUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return model.AttachmentScanResult{Engine: "remote_gateway"}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.AttachmentScanResult{
			Engine:  "remote_gateway",
			Message: fmt.Sprintf("scanner returned HTTP %d", resp.StatusCode),
		}, fmt.Errorf("%w: HTTP %d", ErrAttachmentScannerUnavailable, resp.StatusCode)
	}

	var result model.AttachmentScanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return model.AttachmentScanResult{Engine: "remote_gateway"}, fmt.Errorf("%w: %v", ErrAttachmentScannerResponse, err)
	}
	result.Engine = strings.TrimSpace(result.Engine)
	result.Message = strings.TrimSpace(result.Message)
	if result.Engine == "" {
		return model.AttachmentScanResult{Engine: "remote_gateway"}, ErrAttachmentScannerResponse
	}
	return result, nil
}
