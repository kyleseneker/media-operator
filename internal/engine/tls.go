package engine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/reconciler"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveTLSConfig builds a *tls.Config from a TLSConfig CRD spec.
// Returns nil if tlsCfg is nil and no TLS customization is needed.
func ResolveTLSConfig(ctx context.Context, c client.Reader, namespace string, tlsCfg *commonv1alpha1.TLSConfig) (*tls.Config, error) {
	if tlsCfg == nil {
		return nil, nil
	}

	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	if tlsCfg.InsecureSkipVerify {
		cfg.InsecureSkipVerify = true
	}

	if tlsCfg.CASecretRef != nil {
		caPEM, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *tlsCfg.CASecretRef)
		if err != nil {
			return nil, fmt.Errorf("resolving CA certificate secret: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return nil, fmt.Errorf("CA certificate secret %s/%s key %q contains no valid PEM certificates", namespace, tlsCfg.CASecretRef.Name, tlsCfg.CASecretRef.Key)
		}
		cfg.RootCAs = pool
	}

	return cfg, nil
}

// NewHTTPTransport creates an *http.Transport with SSRF protection and optional TLS configuration.
// If tlsCfg is nil, the default TLS settings are used.
func NewHTTPTransport(tlsCfg *tls.Config) *http.Transport {
	return &http.Transport{
		DialContext:     ssrfSafeDialContext,
		TLSClientConfig: tlsCfg,
	}
}
