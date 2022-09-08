package benchmarks

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"k8s.io/kube-openapi/pkg/jsonstream"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

const openapipath = "testdata/swagger.json" // https://github.com/kubernetes/kubernetes/raw/master/api/openapi-spec/swagger.json
func BenchmarkJsonUnmarshalSwagger(b *testing.B) {
	content, err := os.ReadFile(openapipath)
	if err != nil {
		b.Fatalf("Failed to open file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := spec.Swagger{}
		err := json.Unmarshal(content, &t)
		if err != nil {
			b.Fatalf("Failed to unmarshal: %v", err)
		}
	}
}

func BenchmarkJsonUnmarshalInterface(b *testing.B) {
	content, err := os.ReadFile(openapipath)
	if err != nil {
		b.Fatalf("Failed to open file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := map[string]interface{}{}
		err := json.Unmarshal(content, &t)
		if err != nil {
			b.Fatalf("Failed to unmarshal: %v", err)
		}
	}
}

func BenchmarkJsonUnmarshalStream(b *testing.B) {
	content, err := os.ReadFile(openapipath)
	if err != nil {
		b.Fatalf("Failed to open file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := spec.Swagger{}
		err := jsonstream.Unmarshal(content, &t)
		if err != nil {
			b.Fatalf("Failed to unmarshal: %v", err)
		}
	}
}

func TestSameResults(t *testing.T) {
	content, err := os.ReadFile(openapipath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}

	t1 := spec.Swagger{}
	{
		err := jsonstream.Unmarshal(content, &t1)
		if err != nil {
			t.Fatalf("jsonstream.Unmarshal failed: %v", err)
		}
	}

	t2 := spec.Swagger{}
	{
		err := json.Unmarshal(content, &t2)
		if err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}
	}

	j1, err := json.Marshal(t1)
	if err != nil {
		t.Fatalf("json.Marshal(t1) failed: %v", err)
	}
	j2, err := json.Marshal(t2)
	if err != nil {
		t.Fatalf("json.Marshal(t2) failed: %v", err)
	}
	s1 := string(j1)
	s2 := string(j2)
	if diff := cmp.Diff(s1, s2); diff != "" {
		t.Errorf("diff:\n%v", diff)
	}

	if !reflect.DeepEqual(t1, t2) {
		t.Errorf("Not deep equal")
	}
}
