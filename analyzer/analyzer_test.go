package analyzer

import (
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func Test(t *testing.T) {
	path, _ := filepath.Abs("../samples")

	analysistest.Run(t, path, Analyzer, "http-response-ignored", "http-response-not-assigned", "multi-assign", "http-response-on-go-statement", "http-response-on-defer-statement", "http-response-assigned")
}
