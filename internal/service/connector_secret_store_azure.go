package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-simple-task-api/internal/model"
)

const azureKeyVaultAPIVersion = "7.4"

type AzureKeyVaultConfig struct {
	VaultURL                string
	Prefix                  string
	AccessToken             string
	TenantID                string
	ClientID                string
	FederatedTokenFile      string
	AuthorityHost           string
	UseManagedIdentity      bool
	ManagedIdentityEndpoint string
	CMKKeyID                string
	Timeout                 time.Duration
	AllowInsecure           bool
}

type azureAccessTokenProvider interface {
	Token(context.Context) (string, error)
}

type azureStaticTokenProvider struct {
	token string
}

func (p azureStaticTokenProvider) Token(context.Context) (string, error) {
	if strings.TrimSpace(p.token) == "" {
		return "", fmt.Errorf("%w: Azure access token is not configured", ErrConnectorSecretStore)
	}
	return p.token, nil
}

type AzureKeyVaultSecretStore struct {
	vaultURL *url.URL
	prefix   string
	cmkKeyID *url.URL
	client   *http.Client
	tokens   azureAccessTokenProvider
}

type azureKeyVaultAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *azureKeyVaultAPIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Azure Key Vault %s (HTTP %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("Azure Key Vault %s: %s", e.Code, e.Message)
}

type azureStoredConnectorSecret struct {
	Payload  string `json:"payload"`
	Version  int    `json:"version"`
	Encoding string `json:"encoding"`
	KeyID    string `json:"key_id,omitempty"`
}

func (s *AzureKeyVaultSecretStore) Backend() string {
	return model.ConnectorSecretBackendAzure
}
