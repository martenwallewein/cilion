/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
Copyright 2026.
*/

package main

import (
	"crypto/tls"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	cilionv1alpha1 "github.com/martenwallewein/cilion/api/v1alpha1"
	"github.com/martenwallewein/cilion/internal/controller"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(cilionv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableLeaderElection bool
	var secureMetrics bool
	var enableHTTP2 bool
	var mode string // <-- NEW: The Mode Flag

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true, "Serve metrics securely via HTTPS.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false, "Enable HTTP/2 for the metrics and webhook servers")

	// NEW: Define the mode flag
	flag.StringVar(&mode, "mode", "operator", "Run mode: 'operator' (global control plane) or 'agent' (local node daemon)")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// 1. Mode-Specific Configurations
	if mode == "agent" {
		// IMPORTANT: A DaemonSet runs on EVERY node. If you enable leader election,
		// 99 nodes will be asleep and only 1 will inject eBPF.
		// Agents must NEVER use leader election.
		setupLog.Info("Forcing leader election OFF for Agent mode")
		enableLeaderElection = false
	}

	// 2. Cache Filtering (Protect the Agent from crashing)
	var cacheOpts cache.Options
	if mode == "agent" {
		nodeName := os.Getenv("NODE_NAME")
		if nodeName != "" {
			setupLog.Info("Agent Mode: Restricting Pod cache to local node", "node", nodeName)
			// The Agent only cares about Pods running on its own physical machine!
			cacheOpts = cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&corev1.Pod{}: {
						Field: fields.SelectorFromSet(fields.Set{"spec.nodeName": nodeName}),
					},
				},
			}
		} else {
			setupLog.Info("WARNING: NODE_NAME env var is not set. Agent will cache all cluster Pods (High Memory Usage).")
		}
	}

	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}
	var tlsOpts []func(*tls.Config)
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}
	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// 3. Start the Manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhook.NewServer(webhook.Options{TLSOpts: tlsOpts}),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "db004011.operator.cilion.io",
		Cache:                  cacheOpts, // Inject the restricted cache
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	// 4. Branch Controller Registration based on Mode
	switch mode {
	case "operator":
		setupLog.Info("Starting in OPERATOR Mode (Global Control Plane)")

		// The Operator computes paths. It watches Intent (Policies) and Destinations (Peers).
		if err := (&controller.ScionPathPolicyReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create controller", "controller", "ScionPathPolicy")
			os.Exit(1)
		}

		if err := (&controller.ScionClusterPeerReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create controller", "controller", "ScionClusterPeer")
			os.Exit(1)
		}

	case "agent":
		setupLog.Info("Starting in AGENT Mode (Local Node eBPF Datapath)")

		// The Agent only consumes pre-computed state and watches local pods.
		if err := (&controller.ScionComputedPathReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
			// EBPFManager: eBPFManager, <-- You will initialize and pass your eBPF loader here
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Failed to create controller", "controller", "ScionComputedPath")
			os.Exit(1)
		}

	default:
		setupLog.Error(nil, "Invalid mode specified. Must be 'operator' or 'agent'", "mode", mode)
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}
