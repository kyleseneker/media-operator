package pvr

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	pvrclient "github.com/kyleseneker/media-operator/internal/client/pvr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
)

type SonarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=sonarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=sonarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=sonarrconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *SonarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config pvrv1alpha1.SonarrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	s := config.Spec
	return reconcilePVR(ctx, r.Client, r.Recorder, pvrConfig{
		app: pvrclient.Sonarr, obj: &config, spec: s, connection: s.Connection, policy: s.Reconcile,
		options: pvrclient.Options{
			RootFolders:        s.RootFolders,
			RemotePathMappings: s.RemotePathMappings,
			DownloadClients:    s.DownloadClients,
			QualityProfiles:    s.QualityProfiles,
			CustomFormats:      s.CustomFormats,
			Tags:               s.Tags,
			Indexers:           s.Indexers,
			Notifications:      s.Notifications,
			ImportLists:        s.ImportLists,
		},
	})
}

func (r *SonarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pvrv1alpha1.SonarrConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &pvrv1alpha1.SonarrConfigList{})).
		Named("sonarrconfig").
		Complete(r)
}
