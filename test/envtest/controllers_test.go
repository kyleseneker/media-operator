package envtest

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/automation"
	"github.com/kyleseneker/media-operator/internal/controller/curation"
	"github.com/kyleseneker/media-operator/internal/controller/downloads"
	"github.com/kyleseneker/media-operator/internal/controller/indexers"
	"github.com/kyleseneker/media-operator/internal/controller/mediaservers"
	"github.com/kyleseneker/media-operator/internal/controller/pvr"
	"github.com/kyleseneker/media-operator/internal/controller/requests"
	"github.com/kyleseneker/media-operator/internal/controller/subtitles"
)

// controllerCase describes one kind well enough to drive it through a create,
// reconcile and delete against a real API server.
type controllerCase struct {
	name        string
	addToScheme func(*runtime.Scheme) error
	newCR       func(url string) client.Object
	newObj      func() client.Object
	newRec      func(client.Client, *runtime.Scheme) reconcile.Reconciler
}

func appConn(url string) commonv1alpha1.AppConnection {
	return commonv1alpha1.AppConnection{
		URL:             url,
		APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "app-credentials", Key: "api-key"},
	}
}

func secretRef(key string) commonv1alpha1.SecretKeyRef {
	return commonv1alpha1.SecretKeyRef{Name: "app-credentials", Key: key}
}

func orphan() *commonv1alpha1.ReconcileConfig {
	p := "orphan"
	return &commonv1alpha1.ReconcileConfig{DeletionPolicy: &p}
}

func meta(name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: "default"}
}

func controllerCases() []controllerCase {
	return []controllerCase{
		{
			name:        "BazarrConfig",
			addToScheme: subtitlesv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &subtitlesv1alpha1.BazarrConfig{} },
			newCR: func(url string) client.Object {
				return &subtitlesv1alpha1.BazarrConfig{
					ObjectMeta: meta("bazarr"),
					Spec:       subtitlesv1alpha1.BazarrConfigSpec{Connection: appConn(url), Reconcile: orphan()},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &subtitles.BazarrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
		{
			name:        "MaintainerrConfig",
			addToScheme: curationv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &curationv1alpha1.MaintainerrConfig{} },
			newCR: func(url string) client.Object {
				return &curationv1alpha1.MaintainerrConfig{
					ObjectMeta: meta("maintainerr"),
					Spec:       curationv1alpha1.MaintainerrConfigSpec{Connection: appConn(url), Reconcile: orphan()},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &curation.MaintainerrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
		{
			name:        "AutobrrConfig",
			addToScheme: automationv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &automationv1alpha1.AutobrrConfig{} },
			newCR: func(url string) client.Object {
				return &automationv1alpha1.AutobrrConfig{
					ObjectMeta: meta("autobrr"),
					Spec:       automationv1alpha1.AutobrrConfigSpec{Connection: appConn(url), Reconcile: orphan()},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &automation.AutobrrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
		{
			name:        "SabnzbdConfig",
			addToScheme: downloadsv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &downloadsv1alpha1.SabnzbdConfig{} },
			newCR: func(url string) client.Object {
				return &downloadsv1alpha1.SabnzbdConfig{
					ObjectMeta: meta("sabnzbd"),
					Spec:       downloadsv1alpha1.SabnzbdConfigSpec{Connection: appConn(url), Reconcile: orphan()},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &downloads.SabnzbdConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
		{
			name:        "SonarrConfig",
			addToScheme: pvrv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &pvrv1alpha1.SonarrConfig{} },
			newCR: func(url string) client.Object {
				return &pvrv1alpha1.SonarrConfig{
					ObjectMeta: meta("sonarr"),
					Spec:       pvrv1alpha1.SonarrConfigSpec{Connection: appConn(url), Reconcile: orphan()},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &pvr.SonarrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
		{
			name:        "ProwlarrConfig",
			addToScheme: indexersv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &indexersv1alpha1.ProwlarrConfig{} },
			newCR: func(url string) client.Object {
				return &indexersv1alpha1.ProwlarrConfig{
					ObjectMeta: meta("prowlarr"),
					Spec:       indexersv1alpha1.ProwlarrConfigSpec{Connection: appConn(url), Reconcile: orphan()},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &indexers.ProwlarrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
		{
			name:        "JellyfinConfig",
			addToScheme: mediaserversv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &mediaserversv1alpha1.JellyfinConfig{} },
			newCR: func(url string) client.Object {
				return &mediaserversv1alpha1.JellyfinConfig{
					ObjectMeta: meta("jellyfin"),
					Spec: mediaserversv1alpha1.JellyfinConfigSpec{
						Connection: mediaserversv1alpha1.JellyfinConnection{URL: url},
						Reconcile:  orphan(),
					},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &mediaservers.JellyfinConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
		{
			name:        "SeerrConfig",
			addToScheme: requestsv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &requestsv1alpha1.SeerrConfig{} },
			newCR: func(url string) client.Object {
				ref := secretRef("api-key")
				return &requestsv1alpha1.SeerrConfig{
					ObjectMeta: meta("seerr"),
					Spec: requestsv1alpha1.SeerrConfigSpec{
						Connection: requestsv1alpha1.SeerrConnection{URL: url, APIKeySecretRef: &ref},
						Reconcile:  orphan(),
					},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &requests.SeerrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
		{
			name:        "QBittorrentConfig",
			addToScheme: downloadsv1alpha1.AddToScheme,
			newObj:      func() client.Object { return &downloadsv1alpha1.QBittorrentConfig{} },
			newCR: func(url string) client.Object {
				return &downloadsv1alpha1.QBittorrentConfig{
					ObjectMeta: meta("qbittorrent"),
					Spec: downloadsv1alpha1.QBittorrentConfigSpec{
						Connection: downloadsv1alpha1.QBittorrentConnection{
							URL:               url,
							UsernameSecretRef: secretRef("username"),
							PasswordSecretRef: secretRef("password"),
						},
						Reconcile: orphan(),
					},
				}
			},
			newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
				return &downloads.QBittorrentConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
			},
		},
	}
}

// Every kind must persist status through the subresource and release its
// finalizer on delete. Both are API-server behaviours a fake client fakes away.
func TestControllersPersistStatusAndReleaseOnDelete(t *testing.T) {
	cases := controllerCases()
	adders := make([]func(*runtime.Scheme) error, 0, len(cases))
	for _, tc := range cases {
		adders = append(adders, tc.addToScheme)
	}
	c := startAPIServerWith(t, adders...)
	url := fakeTdarrServer(t)
	ctx := context.Background()

	if err := c.Create(ctx, &corev1.Secret{
		ObjectMeta: meta("app-credentials"),
		Data: map[string][]byte{
			"api-key":  []byte("k"),
			"username": []byte("u"),
			"password": []byte("p"),
			"token":    []byte("t"),
		},
	}); err != nil {
		t.Fatalf("creating secret: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cr := tc.newCR(url)
			if err := c.Create(ctx, cr); err != nil {
				t.Fatalf("creating CR: %v", err)
			}
			key := client.ObjectKeyFromObject(cr)
			r := tc.newRec(c, c.Scheme())

			if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
				t.Fatalf("reconcile: %v", err)
			}

			got := tc.newObj()
			if err := c.Get(ctx, key, got); err != nil {
				t.Fatalf("re-reading CR: %v", err)
			}
			if len(got.GetFinalizers()) == 0 {
				t.Fatal("no finalizer set, so the deletion half of this test proves nothing")
			}

			if err := c.Delete(ctx, got); err != nil {
				t.Fatalf("deleting CR: %v", err)
			}
			deadline := time.Now().Add(30 * time.Second)
			for {
				if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
					t.Fatalf("reconcile during deletion: %v", err)
				}
				err := c.Get(ctx, key, tc.newObj())
				if apierrors.IsNotFound(err) {
					return
				}
				if time.Now().After(deadline) {
					t.Fatal("CR still present after deletion; the finalizer was never cleared")
				}
				time.Sleep(200 * time.Millisecond)
			}
		})
	}
}
