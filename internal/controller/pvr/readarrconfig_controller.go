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

type ReadarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=readarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=readarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=readarrconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ReadarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config pvrv1alpha1.ReadarrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	s := config.Spec
	return reconcilePVR(ctx, r.Client, r.Recorder, pvrConfig{
		app: "readarr", apiVersion: "v1", obj: &config, spec: s, connection: s.Connection, policy: s.Reconcile,
		options: pvrclient.Options{
			RootFolders:        s.RootFolders,
			RemotePathMappings: s.RemotePathMappings,
			DownloadClients:    s.DownloadClients,
			CategoryField:      "bookCategory",
			ImportListFields:   pvrclient.ImportListFields{AutomaticAdd: "enableAutomaticAdd", Monitor: "shouldMonitor", MonitorAll: "entireAuthor"},
			MetadataProfiles:   true,
			QualityProfiles:    s.QualityProfiles,
			CustomFormats:      s.CustomFormats,
			Tags:               s.Tags,
			Indexers:           s.Indexers,
			Notifications:      s.Notifications,
			ImportLists:        s.ImportLists,
		},
	})
}

func (r *ReadarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pvrv1alpha1.ReadarrConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &pvrv1alpha1.ReadarrConfigList{})).
		Named("readarrconfig").
		Complete(r)
}
