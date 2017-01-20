package api

import (
	"bytes"
	"fmt"
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type HelpersTestSuit struct {
	suite.Suite
	assert *assertPackage.Assertions
	testID string
}

// SetUp/Teardown
func (s *HelpersTestSuit) SetupTest() {
	// Setup assert function
	s.assert = assertPackage.New(s.T())

	// Set a unique test ID
	s.testID = fmt.Sprintf("tmp-%d", time.Now().UnixNano())
}
func (s *HelpersTestSuit) TearDownTest() {}

//Tests
func (s *HelpersTestSuit) TestReadFileNoFile() {
	r, err := readFile("/noFile")
	s.assert.Error(err)
	s.assert.Nil(r)
}

func (s *HelpersTestSuit) TestReadFile() {
	// create a test file
	tempFile, err := ioutil.TempFile("", s.testID)
	if err == nil {
		defer os.Remove(tempFile.Name())
	}
	s.assert.NoError(err)
	tempFile.WriteString(s.testID)
	tempFile.Close()

	r, err := readFile(filepath.Join(tempFile.Name()))
	if err == nil {
		defer r.Close()
	}

	s.assert.NotNil(r)
	s.assert.NoError(err)
	buf := new(bytes.Buffer)
	io.Copy(buf, r)
	s.assert.Equal(buf.String(), s.testID)
}

// Run test suit
func TestHelpersTestSuit(t *testing.T) {
	suite.Run(t, new(HelpersTestSuit))
}
