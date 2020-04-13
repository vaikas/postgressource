/*
Copyright 2020 The Knative Authors

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

package resources

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"
	"knative.dev/pkg/tracker"

	bindingsv1alpha1 "github.com/mattmoor/bindings/pkg/apis/bindings/v1alpha1"
	"github.com/vaikas/postgressource/pkg/apis/sources/v1alpha1"
	"github.com/vaikas/postgressource/pkg/reconciler/postgressource/resources/names"
)

func MakePostgresBinding(ctx context.Context, src *v1alpha1.PostgresSource) *bindingsv1alpha1.SQLBinding {
	return &bindingsv1alpha1.SQLBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            kmeta.ChildName(src.Name, "-sqlbinding"),
			Namespace:       src.Namespace,
			OwnerReferences: []metav1.OwnerReference{*kmeta.NewControllerRef(src)},
		},
		Spec: bindingsv1alpha1.SQLBindingSpec{
			Secret: corev1.LocalObjectReference{Name: src.Spec.Secret.Name},
			// Bind to the Deployment for the receive adapter.
			BindingSpec: duckv1alpha1.BindingSpec{
				Subject: tracker.Reference{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Namespace:  src.Namespace,
					Name:       names.Deployment(vms),
				},
			},
		},
	}
}
