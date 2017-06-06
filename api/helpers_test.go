package api

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type HelpersTestSuite struct {
	suite.Suite
	assert *assertPackage.Assertions
	testID string
}

// SetUp/Teardown
func (s *HelpersTestSuite) SetupTest() {
	// Setup assert function
	s.assert = assertPackage.New(s.T())

	// Set a unique test ID
	s.testID = fmt.Sprintf("tmp-%d", time.Now().UnixNano())
}
func (s *HelpersTestSuite) TearDownTest() {}

//Tests
func (s *HelpersTestSuite) TestReadFileNoFile() {
	r, err := readFile("/noFile")
	s.assert.Error(err)
	s.assert.Nil(r)
}

func (s *HelpersTestSuite) TestReadFile() {
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
	suite.Run(t, new(HelpersTestSuite))
}
