package spec

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func getTempFile(content string) *os.File {
	tmpfile, _ := ioutil.TempFile("", "")
	tmpfile.WriteString(content)
	tmpfile.Seek(0, 0)
	return tmpfile
}

func TestRead(t *testing.T) {
	bads := []string{
		"",
		"version:3",
		`
version: 3
services:
`,
		`
version: 3
services:
  name1:
    instances: 42
`,
	}

	for _, bad := range bads {
		f := getTempFile(bad)
		defer os.Remove(f.Name())
		s := Spec{}
		assert.Error(t, s.Read(f))
	}

	goods := map[int]string{
		1: `
version: 3
services:
  name1:
    instances: 1
    image: image1
`,
		2: `
version: 3
services:
  name1:
    instances: 1
    image: image1
  name2:
    image: image2
    instances: 2
`,
	}

	for size, good := range goods {
		f := getTempFile(good)
		defer os.Remove(f.Name())
		s := Spec{}
		assert.NoError(t, s.Read(f))
		assert.Equal(t, size, len(s.JobSpecs()))
		for _, jobSpec := range s.JobSpecs() {
			assert.Equal(t, fmt.Sprintf("name%d", jobSpec.GetService().Instances), jobSpec.Meta.Name)
			assert.Equal(t, fmt.Sprintf("image%d", jobSpec.GetService().Instances), jobSpec.Template.GetContainer().Image.Reference)
		}
	}
}

func TestSpecDiff(t *testing.T) {
	spec := &Spec{
		Version:   3,
		Namespace: "namespace",
		Services: map[string]*ServiceConfig{
			"name1": {Instances: 1, ContainerConfig: ContainerConfig{Image: "img1"}},
			"name2": {Instances: 1, ContainerConfig: ContainerConfig{Image: "img2"}},
		},
	}

	diff, err := spec.Diff(0, "remote", "local",
		&Spec{
			Version:   3,
			Namespace: "namespace",
			Services: map[string]*ServiceConfig{
				"name1": {Instances: 2, ContainerConfig: ContainerConfig{Image: "img1"}},
				"name2": {Instances: 3, ContainerConfig: ContainerConfig{Image: "img2"}},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, "--- remote\n+++ local\n@@ -6 +6 @@\n-    instances: 2\n+    instances: 1\n@@ -9 +9 @@\n-    instances: 3\n+    instances: 1\n", diff)

	diff, err = spec.Diff(0, "old", "new",
		&Spec{
			Version:   3,
			Namespace: "namespace",
			Services: map[string]*ServiceConfig{
				"name1": {Instances: 1, ContainerConfig: ContainerConfig{Image: "img3"}},
				"name2": {Instances: 1, ContainerConfig: ContainerConfig{Image: "img2"}},
			},
		},
	)

	assert.NoError(t, err)
	assert.Equal(t, "--- old\n+++ new\n@@ -5 +5 @@\n-    image: img3\n+    image: img1\n", diff)
}
