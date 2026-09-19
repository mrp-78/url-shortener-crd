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

package controller

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	shortenerv1 "github.com/learn/kuber-crd/operator/api/v1"
	shortenerclient "github.com/learn/kuber-crd/operator/internal/client"
)

const (
	URLShortenerFinalizer = "shortener.tapsi.cloud/finalizer"
	DefaultPollInterval   = 10 * time.Second
)

// URLShortenerReconciler reconciles a URLShortener object
type URLShortenerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	BackendClient *shortenerclient.BackendClient
	PollInterval  time.Duration
}

// +kubebuilder:rbac:groups=shortener.tapsi.cloud,resources=urlshorteners,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=shortener.tapsi.cloud,resources=urlshorteners/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=shortener.tapsi.cloud,resources=urlshorteners/finalizers,verbs=update

func (r *URLShortenerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var urlShortener shortenerv1.URLShortener
	if err := r.Get(ctx, req.NamespacedName, &urlShortener); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	pollInterval := r.PollInterval
	if pollInterval == 0 {
		pollInterval = DefaultPollInterval
	}

	// 1. Handle Finalizer & Deletion
	if !urlShortener.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&urlShortener, URLShortenerFinalizer) {
			logger.Info("Deleting short URL from backend service", "slug", urlShortener.Status.Slug)
			if urlShortener.Status.Slug != "" && r.BackendClient != nil {
				if err := r.BackendClient.DeleteURL(ctx, urlShortener.Status.Slug); err != nil {
					logger.Error(err, "Failed to delete URL from backend service")
					return ctrl.Result{RequeueAfter: 5 * time.Second}, err
				}
			}
			controllerutil.RemoveFinalizer(&urlShortener, URLShortenerFinalizer)
			if err := r.Update(ctx, &urlShortener); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// 2. Ensure Finalizer is present
	if !controllerutil.ContainsFinalizer(&urlShortener, URLShortenerFinalizer) {
		controllerutil.AddFinalizer(&urlShortener, URLShortenerFinalizer)
		if err := r.Update(ctx, &urlShortener); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 3. Register URL if not yet assigned a slug
	if urlShortener.Status.Slug == "" {
		logger.Info("Registering URL with backend service", "targetUrl", urlShortener.Spec.TargetURL)
		if r.BackendClient == nil {
			logger.Error(nil, "BackendClient is nil")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		resp, err := r.BackendClient.CreateURL(ctx, urlShortener.Spec.TargetURL, urlShortener.Spec.CustomSlug)
		if err != nil {
			logger.Error(err, "Failed to register URL with backend service")
			urlShortener.Status.Phase = "Error"
			meta.SetStatusCondition(&urlShortener.Status.Conditions, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "BackendRegistrationFailed",
				Message: err.Error(),
			})
			_ = r.Status().Update(ctx, &urlShortener)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		urlShortener.Status.Slug = resp.Slug
		urlShortener.Status.ShortURL = resp.ShortURL
		urlShortener.Status.Hits = resp.Hits
		urlShortener.Status.Phase = "Ready"
		meta.SetStatusCondition(&urlShortener.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionTrue,
			Reason:  "Registered",
			Message: "Successfully registered URL with backend service",
		})
		if err := r.Status().Update(ctx, &urlShortener); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: pollInterval}, nil
	}

	// 4. Poll Hits Telemetry
	if r.BackendClient != nil {
		stats, err := r.BackendClient.GetStats(ctx, urlShortener.Status.Slug)
		if err != nil {
			logger.Error(err, "Failed to fetch stats from backend service", "slug", urlShortener.Status.Slug)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// Optimization: Only update status if hits changed
		if stats.Hits != urlShortener.Status.Hits {
			logger.Info("Hits changed, updating status", "oldHits", urlShortener.Status.Hits, "newHits", stats.Hits)
			urlShortener.Status.Hits = stats.Hits
			now := metav1.Now()
			urlShortener.Status.LastPolledTime = &now
			if err := r.Status().Update(ctx, &urlShortener); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *URLShortenerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&shortenerv1.URLShortener{}).
		Named("urlshortener").
		Complete(r)
}
