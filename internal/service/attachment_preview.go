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
	ErrAttachmentPreviewConfiguration = errors.New("invalid attachment preview configuration")
	ErrAttachmentPreviewUnavailable   = errors.New("attachment preview unavailable")
	ErrAttachmentPreviewResponse      = errors.New("invalid attachment preview response")
)

type AttachmentPreviewer interface {
	Enabled() bool
	Generate(context.Context, model.Attachment, string) (model.AttachmentPreviewResult, error)
}

type NoopAttachmentPreviewer struct{}

func (NoopAttachmentPreviewer) Enabled() bool { return false }

func (NoopAttachmentPreviewer) Generate(context.Context, model.Attachment, string) (model.AttachmentPreviewResult, error) {
	return model.AttachmentPreviewResult{}, ErrAttachmentPreviewUnavailable
}

type HTTPAttachmentPreviewer struct {
	endpoint      *url.URL
	bearerToken   string
	signingSecret []byte
	client        *http.Client
}

type attachmentPreviewRequest struct {
	AttachmentID     int64  `json:"attachment_id"`
	WorkspaceID      int64  `json:"workspace_id"`
	Provider         string `json:"provider"`
	Bucket           string `json:"bucket"`
	ObjectKey        string `json:"object_key"`
	PreviewObjectKey string `json:"preview_object_key"`
	FileName         string `json:"file_name"`
	ContentType      string `json:"content_type"`
	SizeBytes        int64  `json:"size_bytes"`
	SHA256           string `json:"sha256"`
	Encryption       string `json:"encryption,omitempty"`
	EncryptionKeyID  string `json:"encryption_key_id,omitempty"`
	MaxWidth         int    `json:"max_width"`
	MaxHeight        int    `json:"max_height"`
}

func NewAttachmentPreviewer(cfg AttachmentConfig) (AttachmentPreviewer, error) {
	rawURL := strings.TrimSpace(cfg.PreviewURL)
	if rawURL == "" {
		if cfg.PreviewRequired {
			return nil, fmt.Errorf("%w: ATTACHMENT_PREVIEW_URL is required", ErrAttachmentPreviewConfiguration)
		}
		return NoopAttachmentPreviewer{}, nil
	}
	endpoint, err := url.Parse(rawURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, ErrAttachmentPreviewConfiguration
	}
	if endpoint.Scheme != "https" && !(cfg.AllowInsecure && endpoint.Scheme == "http") {
		return nil, ErrAttachmentPreviewConfiguration
	}
	timeout := cfg.PreviewTimeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &HTTPAttachmentPreviewer{
		endpoint:      endpoint,
		bearerToken:   strings.TrimSpace(cfg.PreviewBearerToken),
		signingSecret: []byte(strings.TrimSpace(cfg.PreviewSigningSecret)),
		client:        client,
	}, nil
}

func (p *HTTPAttachmentPreviewer) Enabled() bool { return true }

func (p *HTTPAttachmentPreviewer) Generate(ctx context.Context, attachment model.Attachment, previewObjectKey string) (model.AttachmentPreviewResult, error) {
	payload, err := json.Marshal(attachmentPreviewRequest{
		AttachmentID:     attachment.ID,
		WorkspaceID:      attachment.WorkspaceID,
		Provider:         attachment.Provider,
		Bucket:           attachment.Bucket,
		ObjectKey:        attachment.ObjectKey,
		PreviewObjectKey: previewObjectKey,
		FileName:         attachment.FileName,
		ContentType:      attachment.ContentType,
		SizeBytes:        attachment.SizeBytes,
		SHA256:           attachment.SHA256,
		Encryption:       attachment.Encryption,
		EncryptionKeyID:  attachment.EncryptionKeyID,
		MaxWidth:         1600,
		MaxHeight:        1600,
	})
	if err != nil {
		return model.AttachmentPreviewResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return model.AttachmentPreviewResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "simple-task-manager-attachment-preview/1")
	if p.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.bearerToken)
	}
	if len(p.signingSecret) > 0 {
		timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
		mac := hmac.New(sha256.New, p.signingSecret)
		_, _ = mac.Write([]byte(timestamp))
		_, _ = mac.Write([]byte("\n"))
		_, _ = mac.Write(payload)
		req.Header.Set("X-Attachment-Preview-Timestamp", timestamp)
		req.Header.Set("X-Attachment-Preview-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return model.AttachmentPreviewResult{}, fmt.Errorf("%w: %v", ErrAttachmentPreviewUnavailable, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return model.AttachmentPreviewResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.AttachmentPreviewResult{}, fmt.Errorf("%w: HTTP %d", ErrAttachmentPreviewUnavailable, resp.StatusCode)
	}

	var result model.AttachmentPreviewResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return model.AttachmentPreviewResult{}, fmt.Errorf("%w: %v", ErrAttachmentPreviewResponse, err)
	}
	result.ObjectKey = strings.TrimSpace(result.ObjectKey)
	result.ContentType = strings.ToLower(strings.TrimSpace(result.ContentType))
	result.Engine = strings.TrimSpace(result.Engine)
	result.Message = strings.TrimSpace(result.Message)
	if result.ObjectKey != previewObjectKey ||
		(result.ContentType != "image/png" && result.ContentType != "image/jpeg" && result.ContentType != "image/webp") ||
		result.Width <= 0 || result.Width > 8192 || result.Height <= 0 || result.Height > 8192 || result.Engine == "" {
		return model.AttachmentPreviewResult{}, ErrAttachmentPreviewResponse
	}
	return result, nil
}
