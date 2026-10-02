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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestSplitSummary(t *testing.T) {
	summary, notes := splitSummary("Kind does one thing, e.g. this.\nIt also does\nanother.\n\nSecond paragraph.")
	require.Equal(t, "Kind does one thing, e.g. this.", summary)
	require.Equal(t, []string{"It also does another.", "Second paragraph."}, notes)

	summary, notes = splitSummary("No period")
	require.Equal(t, "No period", summary)
	require.Empty(t, notes)
}

func TestInline(t *testing.T) {
	cases := map[string]struct {
		in      string
		inTable bool
		want    string
	}{
		"escapes markup":          {"a <name> *b* [c] _d_ #e", false, `a \<name\> \*b\* \[c\] \_d\_ \#e`},
		"keeps code spans":        {"see `a<b>_c` here", false, "see `a<b>_c` here"},
		"pipe in a table":         {"a|b `c|d`", true, "a\\|b `c\\|d`"},
		"unpaired backtick":       {"a ` b", false, "a \\` b"},
		"links a citation":        {"guard (0015:D3/D16).", false, "guard ([0015:D3/D16](/enhancements/0015/decisions/))."},
		"links a requirement":     {"see 0011:D9:R2", false, "see [0011:D9:R2](/enhancements/0011/decisions/)"},
		"leaves code citation be": {"`0015:D3`", false, "`0015:D3`"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, inline(tc.in, tc.inTable))
		})
	}
}

func TestSplice(t *testing.T) {
	page := "intro\n\n" + beginMarker + "\nold\n" + endMarker + "\n\nafter\n"
	got, err := splice(page, "new\n")
	require.NoError(t, err)
	require.Equal(t, "intro\n\n"+beginMarker+"\n\nnew\n\n"+endMarker+"\n\nafter\n", got)

	again, err := splice(got, "new\n")
	require.NoError(t, err)
	require.Equal(t, got, again, "splicing the same block twice changes nothing")

	_, err = splice("no markers\n", "new\n")
	require.Error(t, err)
	_, err = splice(endMarker+"\n"+beginMarker+"\n", "new\n")
	require.Error(t, err)
}

func TestTypeOf(t *testing.T) {
	str := apiextensionsv1.JSONSchemaProps{Type: "string"}
	obj := apiextensionsv1.JSONSchemaProps{Type: "object", Properties: map[string]apiextensionsv1.JSONSchemaProps{
		"version": str,
	}}
	preserve := true
	require.Equal(t, "[]string", typeOf(apiextensionsv1.JSONSchemaProps{
		Type: "array", Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &str},
	}))
	require.Equal(t, "map[string]object", typeOf(apiextensionsv1.JSONSchemaProps{
		Type: "object", AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{Schema: &obj},
	}))
	require.Equal(t, "free-form object", typeOf(apiextensionsv1.JSONSchemaProps{
		Type: "object", XPreserveUnknownFields: &preserve,
	}))
	require.Equal(t, "string (date-time)", typeOf(apiextensionsv1.JSONSchemaProps{Type: "string", Format: "date-time"}))
}

// The committed page carries what the generator produces from this tree,
// and producing it twice gives the same bytes.
func TestGenerateIsCurrentAndDeterministic(t *testing.T) {
	root := filepath.Join("..", "..")
	first, err := generate(root)
	require.NoError(t, err)
	second, err := generate(root)
	require.NoError(t, err)
	require.Equal(t, first, second)

	page, err := os.ReadFile(filepath.Join(root, pageFile))
	require.NoError(t, err)
	updated, err := splice(string(page), first)
	require.NoError(t, err)
	require.Equal(t, string(page), updated, "%s is stale: run task dev:docs:reference", pageFile)
	require.True(t, strings.HasSuffix(first, "\n") && !strings.HasSuffix(first, "\n\n"))
}
