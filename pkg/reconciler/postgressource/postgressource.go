/*
Copyright 2019 The Knative Authors

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

package postgressource

import (
	"context"
	"fmt"
	"log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/logging"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/tracker"

	"database/sql"

	"github.com/vaikas/postgressource/pkg/apis/sources/v1alpha1"

	reconcilerpostgressource "github.com/vaikas/postgressource/pkg/client/injection/reconciler/sources/v1alpha1/postgressource"
	"github.com/vaikas/postgressource/pkg/reconciler"
	"github.com/vaikas/postgressource/pkg/reconciler/postgressource/resources"
)

// newReconciledNormal makes a new reconciler event with event type Normal, and
// reason SampleSourceReconciled.
func newReconciledNormal(namespace, name string) pkgreconciler.Event {
	return pkgreconciler.NewEvent(corev1.EventTypeNormal, "PostgresSourceReconciled", "PostgresSource reconciled: \"%s/%s\"", namespace, name)
}

// Reconciler reconciles a PostgresSource object
type Reconciler struct {
	ReceiveAdapterImage string `envconfig:"POSTGRES_SOURCE_RA_IMAGE" required:"true"`

	dr  *reconciler.DeploymentReconciler
	sbr *reconciler.SinkBindingReconciler
	db  *sql.DB
}

// Check that our Reconciler implements Interface
var _ reconcilerpostgressource.Interface = (*Reconciler)(nil)

// ReconcileKind implements Interface.ReconcileKind.
func (r *Reconciler) ReconcileKind(ctx context.Context, src *v1alpha1.PostgresSource) pkgreconciler.Event {
	src.Status.InitializeConditions()
	src.Status.ObservedGeneration = src.Generation

	err := r.reconcileDBFunction(ctx, src)
	if err != nil {
		src.Status.PropagateFunctionCreated(false, err)
		logging.FromContext(ctx).Warnf("Failed to create function: %w", err)
		return err
	}
	src.Status.PropagateFunctionCreated(true, nil)

	for _, table := range src.Spec.Tables {
		logging.FromContext(ctx).Infof("Reconciling table: %q", table.Name)

		// Check that the table exists
		tableExists, err := r.checkTable(ctx, table.Name)
		if err != nil {
			src.Status.PropagateTriggersCreated(false, err)
			logging.FromContext(ctx).Warnf("Couldn't check the existence of table %q: %w", table.Name, err)
			return err
		}
		if !tableExists {
			src.Status.PropagateTriggersCreated(false, fmt.Errorf("Table %q does not exist", table.Name))
			logging.FromContext(ctx).Warnf("Table %q doesn't exist", table.Name)
			return err
		}

		err = r.reconcileDBTrigger(ctx, src, table.Name)
		if err != nil {
			src.Status.PropagateTriggersCreated(false, err)
			logging.FromContext(ctx).Warnf("Failed to reconcile triggers on table %q: %w", table.Name, err)
			return err
		}
		src.Status.PropagateTriggersCreated(true, nil)
	}

	ra, event := r.dr.ReconcileDeployment(ctx, src, resources.MakeReceiveAdapter(&resources.ReceiveAdapterArgs{
		EventSource:         src.Namespace + "/" + src.Name,
		Image:               r.ReceiveAdapterImage,
		NotificationChannel: resources.MakePostgresName(src),
		Source:              src,
		Labels:              resources.Labels(src.Name),
	}))
	if ra != nil {
		src.Status.PropagateDeploymentAvailability(ra)
	}
	if event != nil {
		logging.FromContext(ctx).Infof("returning because event from ReconcileDeployment")
		return event
	}

	if ra != nil {
		logging.FromContext(ctx).Info("going to ReconcileSinkBinding")
		sb, event := r.sbr.ReconcileSinkBinding(ctx, src, src.Spec.SourceSpec, tracker.Reference{
			APIVersion: appsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
			Namespace:  ra.Namespace,
			Name:       ra.Name,
		})
		logging.FromContext(ctx).Infof("ReconcileSinkBinding returned %#v", sb)
		if sb != nil {
			src.Status.MarkSink(sb.Status.SinkURI)
		}
		if event != nil {
			return event
		}
	}

	return newReconciledNormal(src.Namespace, src.Name)
}

// FinalizeKind removes the triggers and functions.
func (r *Reconciler) FinalizeKind(ctx context.Context, src *v1alpha1.PostgresSource) pkgreconciler.Event {
	logging.FromContext(ctx).Infof("IN FINALIZE FOR \"%s/%s\"", src.Namespace, src.Name)
	for _, table := range src.Spec.Tables {
		logging.FromContext(ctx).Infof("Dropping triggers on table: %q", table.Name)
		err := r.dropTriggers(ctx, src, table.Name)
		if err != nil {
			return err
		}
	}
	err := r.dropFunction(ctx, src)
	if err != nil {
		return err
	}
	return newReconciledNormal(src.Namespace, src.Name)
}

// TODO: Diff the function in case it has changed and update it.
func (r *Reconciler) reconcileDBFunction(ctx context.Context, s *v1alpha1.PostgresSource) error {
	exists, err := r.checkFunction(ctx, s)
	if err != nil {
		return err
	}
	if exists {
		log.Printf("Found existing function")
		return nil
	}

	f := resources.MakeFunction(s)
	_, err = r.db.Exec(f)
	if err != nil {
		log.Printf("Failed to create function\n%s\nerr: %v", f, err)
		return err
	}
	return nil

}

func (r *Reconciler) reconcileDBTrigger(ctx context.Context, s *v1alpha1.PostgresSource, table string) error {
	exists, err := r.checkTriggers(ctx, s, table)
	if err != nil {
		return err
	}
	if exists {
		log.Printf("Found existing triggers on table: %q\n", table)
		return nil
	}
	t := resources.MakeTrigger(s, table)
	_, err = r.db.Exec(t)
	if err != nil {
		log.Printf("Failed to create trigger on table: %q\n%s\nerr: %v", table, t, err)
		return err
	}
	return nil

}

// Just make sure the table we're trying to create triggers against table that actually exists.
func (r *Reconciler) checkTable(ctx context.Context, table string) (bool, error) {
	rows, err := r.db.Query(resources.GetTableQuery, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var tableName string
		err = rows.Scan(&tableName)
		if err != nil {
			log.Fatal(err)
		}
		if tableName == table {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (r *Reconciler) checkTriggers(ctx context.Context, src *v1alpha1.PostgresSource, table string) (bool, error) {
	tName := resources.MakePostgresName(src)
	rows, err := r.db.Query(resources.GetTriggersQuery, table, tName)
	var insert, update, delete bool
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var triggerName, cond, table string
		err = rows.Scan(&triggerName, &cond, &table)
		if err != nil {
			return false, err
		}
		switch cond {
		case "INSERT":
			insert = true
		case "DELETE":
			delete = true
		case "UPDATE":
			update = true
		default:
			fmt.Printf("Found unknown action %q in table %q", cond, table)
		}
		fmt.Printf("%q %q %q\n", triggerName, cond, table)
	}
	return insert == true && update == true && delete == true, rows.Err()
}

func (r *Reconciler) checkFunction(ctx context.Context, src *v1alpha1.PostgresSource) (bool, error) {
	fName := resources.MakePostgresName(src)
	rows, err := r.db.Query(resources.GetFunctionQuery, fName)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var functionName string
		err = rows.Scan(&functionName)
		if err != nil {
			return false, err
		}
		fmt.Printf("%q\n", functionName)
		if fName == functionName {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (r *Reconciler) dropFunction(ctx context.Context, src *v1alpha1.PostgresSource) error {
	_, err := r.db.Exec(resources.MakeDropFunction(src))
	return err
}

func (r *Reconciler) dropTriggers(ctx context.Context, src *v1alpha1.PostgresSource, table string) error {
	_, err := r.db.Exec(resources.MakeDropTrigger(src, table))
	return err
}
