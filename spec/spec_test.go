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
	tmpfile.Close()
	return tmpfile
}

func TestReadFrom(t *testing.T) {
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
		_, err := ReadFrom(f.Name())
		assert.Error(t, err)
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
		s, err := ReadFrom(f.Name())
		assert.NoError(t, err)
		assert.Equal(t, size, len(s.JobSpecs()))
		for i, jobSpec := range s.JobSpecs() {
			assert.Equal(t, fmt.Sprintf("name%d", i+1), jobSpec.Meta.Name)
			assert.EqualValues(t, i+1, jobSpec.GetService().Instances)
			assert.Equal(t, fmt.Sprintf("image%d", i+1), jobSpec.Template.GetContainer().Image.Reference)
		}
	}
}
