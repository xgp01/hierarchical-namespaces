package setup

import (
	"fmt"
	"os"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/hierarchical-namespaces/internal/anchor"
	"sigs.k8s.io/hierarchical-namespaces/internal/crd"
	"sigs.k8s.io/hierarchical-namespaces/internal/forest"
	"sigs.k8s.io/hierarchical-namespaces/internal/hierarchyconfig"
	"sigs.k8s.io/hierarchical-namespaces/internal/hncconfig"
	"sigs.k8s.io/hierarchical-namespaces/internal/hrq"
)

type Options struct {
	MaxReconciles   int
	UseFakeClient   bool
	NoWebhooks      bool
	HNCCfgRefresh   time.Duration
	HRQ             bool
	HRQSyncInterval time.Duration
}

func Create(log logr.Logger, mgr ctrl.Manager, f *forest.Forest, opts Options) {
	log.Info("Creating controllers", "opts", opts)

	if !opts.NoWebhooks {
		log.Info("Registering validating webhook (won't work when running locally; use --no-webhooks)")
		createWebhooks(mgr, f, opts)
	}

	log.Info("Registering reconcilers")
	if err := CreateReconcilers(mgr, f, opts); err != nil {
		log.Error(err, "cannot create controllers")
		os.Exit(1)
	}
}

// CreateReconcilers creates all reconcilers.
//
// This function is called both from main.go as well as from the integ tests.
func CreateReconcilers(mgr ctrl.Manager, f *forest.Forest, opts Options) error {
	if err := crd.Setup(mgr, opts.UseFakeClient); err != nil {
		return err
	}

	// Create Anchor reconciler.
	ar := &anchor.Reconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("anchor").WithName("reconcile"),
		Forest: f,
	}
	f.AddListener(ar)

	// Create the HNC Config reconciler.
	hnccfgr := &hncconfig.Reconciler{
		Client:          mgr.GetClient(),
		Log:             ctrl.Log.WithName("hncconfig").WithName("reconcile"),
		Manager:         mgr,
		Forest:          f,
		RefreshDuration: opts.HNCCfgRefresh,
	}

	// Create the HC reconciler with a pointer to the Anchor reconciler.
	hcr := &hierarchyconfig.Reconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("hierarchyconfig").WithName("reconcile"),
		Forest: f,
	}

	if opts.HRQ {
		// Create resource quota reconciler
		rqr := &hrq.ResourceQuotaReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("reconcilers").WithName("ResourceQuota"),
			Forest: f,
		}
		f.AddListener(rqr)

		// Create hierarchical resource quota reconciler
		hrqr := &hrq.HierarchicalResourceQuotaReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("reconcilers").WithName("HierarchicalResourceQuota"),
			Forest: f,
			RQR:    rqr,
		}
		rqr.HRQR = hrqr
		f.AddListener(hrqr)

		if err := rqr.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("cannot create resource quota reconciler: %s", err.Error())
		}
		if err := hrqr.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("cannot create hierarchical resource quota reconciler: %s", err.Error())
		}
		rqr.HRQR = hrqr

		// Create a periodic checker to make sure the forest is not out-of-sync.
		if opts.HRQSyncInterval != 0 {
			go detectIncrementalHRQUsageDrift(f, opts.HRQSyncInterval, hrqr)
		}
	}

	if err := ar.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("cannot create anchor reconciler: %s", err.Error())
	}
	if err := hnccfgr.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("cannot create Config reconciler: %s", err.Error())
	}
	if err := hcr.SetupWithManager(mgr, opts.MaxReconciles); err != nil {
		return fmt.Errorf("cannot create Hierarchy reconciler: %s", err.Error())
	}

	return nil
}

func detectIncrementalHRQUsageDrift(f *forest.Forest, forestSyncInterval time.Duration, hrqr *hrq.HierarchicalResourceQuotaReconciler) {
	syncTicker := time.NewTicker(forestSyncInterval)
	for {
		<-syncTicker.C
		// If there's any out-of-sync, enqueue the affected HRQs to update usages.
		// If not, nothing will be enqueued.
		//hrqr.Enqueue(hrqr.Log, "recover out-of-sync usages", f.CheckSubtreeUsages())
		hrqr.Log.Info("TODO: recover out-of-sync usages")
	}
}
