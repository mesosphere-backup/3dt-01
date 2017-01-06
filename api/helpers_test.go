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

func (s *HelpersTestSuit) TestRunCmdFail() {
	r, err := runCmd([]string{"wrongCommand123"}, 1)
	r.Close()
	s.assert.EqualError(err, `exec: "wrongCommand123": executable file not found in $PATH`)
}

//func (s *HelpersTestSuit) TestRunCmdTimeoutStdout() {
//	startTest := time.Now()
//	r, err := runCmd([]string{"yes"}, 5)
//	defer r.Close()
//	s.assert.NoError(err)
//	buf := new(bytes.Buffer)
//	io.Copy(buf, r)
//	timeElapsed := time.Since(startTest)
//	s.assert.True(timeElapsed.Seconds() >= 5 && timeElapsed.Seconds() <= 10)
//	s.assert.NotEmpty(buf.String())
//}

func (s *HelpersTestSuit) TestRunCmdStdout() {
	r, err := runCmd([]string{"ls", "-la", "/"}, 1)
	defer r.Close()
	s.assert.NoError(err)
	stdoutBuf := new(bytes.Buffer)
	io.Copy(stdoutBuf, r)
	s.assert.NotEmpty(stdoutBuf.String())
}

// Run test suit
func TestHelpersTestSuit(t *testing.T) {
	suite.Run(t, new(HelpersTestSuit))
}
