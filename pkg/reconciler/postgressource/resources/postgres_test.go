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
	"testing"

	"github.com/vaikas/postgressource/pkg/apis/sources/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	expectedFunction = `
CREATE OR REPLACE FUNCTION postgressource_mypostgres_d049318d_72c6_45e3_b8be_b0d4a38e82ad() RETURNS TRIGGER AS $$

    DECLARE 
        data json;
        notification json;
    
    BEGIN
    
        -- Convert the old or new row to JSON, based on the kind of action.
        -- Action = DELETE?             -> OLD row
        -- Action = INSERT or UPDATE?   -> NEW row
        IF (TG_OP = 'DELETE') THEN
            data = row_to_json(OLD);
        ELSE
            data = row_to_json(NEW);
        END IF;
        
        -- Contruct the notification as a JSON string.
        notification = json_build_object(
                          'table',TG_TABLE_NAME,
                          'action', TG_OP,
                          'data', data);
        
                        
        -- Execute pg_notify(channel, notification)
        PERFORM pg_notify('postgressource_mypostgres_d049318d_72c6_45e3_b8be_b0d4a38e82ad',notification::text);
        
        -- Result is ignored since this is an AFTER trigger
        RETURN NULL; 
    END;
    
$$ LANGUAGE plpgsql;
`

	expectedTrigger = `
CREATE TRIGGER postgressource_mypostgres_d049318d_72c6_45e3_b8be_b0d4a38e82ad
AFTER INSERT OR UPDATE OR DELETE ON cloud_events_table
    FOR EACH ROW EXECUTE PROCEDURE postgressource_mypostgres_d049318d_72c6_45e3_b8be_b0d4a38e82ad();
`

	sourceUUID = "d049318d-72c6-45e3-b8be-b0d4a38e82ad"
)

func TestFunction(t *testing.T) {
	f := MakeFunction(&v1alpha1.PostgresSource{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mypostgres",
			UID:  sourceUUID,
		},
	})
	if f != expectedFunction {
		t.Errorf("functions didn't match got:\n%s\nwant:%s\n", f, expectedFunction)
	}
}

func TestTrigger(t *testing.T) {
	f := MakeTrigger(&v1alpha1.PostgresSource{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mypostgres",
			UID:  sourceUUID,
		},
	}, "cloud_events_table")
	if f != expectedTrigger {
		t.Errorf("triggers didn't match got:\n%s\nwant:%s\n", f, expectedTrigger)
	}
}
