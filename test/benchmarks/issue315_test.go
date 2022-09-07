package benchmarks

import (
	"encoding/json"
	"os"
	"testing"

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
