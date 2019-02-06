package main

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"log"
	"os"
	"time"
)

type PackageTest struct {
	Package string
	Test    string
}

type GoTestJSONLine struct {
	PackageTest
	Time    time.Time
	Action  string
	Output  string
	Elapsed float64
}

type Skipped struct{}

type SystemOut struct {
	Output string `xml:",cdata"`
}

type Failure struct {
	Output string `xml:",cdata"`
}

type TestCase struct {
	Name      string     `xml:"name,attr"`
	Time      float64    `xml:"time,attr"`
	Skipped   *Skipped   `xml:"skipped,omitempty"`
	Failure   *Failure   `xml:"failure,omitempty"`
	SystemOut *SystemOut `xml:"system-out,omitempty"`
}

type TestSuite struct {
	Name      string     `xml:"name,attr"`
	Tests     int        `xml:"tests,attr"`
	Skipped   int        `xml:"skipped,attr"`
	Failures  int        `xml:"failures,attr"`
	Time      float64    `xml:"time,attr"`
	TestCases []TestCase `xml:"testcase"`
}

type TestSuites struct {
	XMLName    xml.Name     `xml:"testsuites"`
	TestSuites []*TestSuite `xml:"testsuite"`
}

func (ts *TestSuites) TestSuite(name string) *TestSuite {
	for _, x := range ts.TestSuites {
		if x.Name == name {
			return x
		}
	}
	x := &TestSuite{
		Name: name,
	}
	ts.TestSuites = append(ts.TestSuites, x)
	return x
}

func main() {
	testsuites := TestSuites{}
	partialOutput := map[PackageTest]string{}
	input := json.NewDecoder(os.Stdin)
	for {
		var line GoTestJSONLine
		if err := input.Decode(&line); err != nil {
			if err == io.EOF {
				break
			}
			log.Fatal(err)
		}
		switch line.Action {
		case "output":
			partialOutput[line.PackageTest] += line.Output
		case "pass", "skip", "fail":
			ts := testsuites.TestSuite(line.Package)
			if line.Test == "" {
				ts.Time = line.Elapsed
				if _, ok := partialOutput[line.PackageTest]; !ok {
					continue
				}
				if line.Action == "pass" {
					delete(partialOutput, line.PackageTest)
					continue
				}
			}
			tc := TestCase{
				Name: line.Test,
				Time: line.Elapsed,
			}
			switch line.Action {
			case "skip":
				ts.Skipped++
				tc.Skipped = &Skipped{}
				tc.SystemOut = &SystemOut{
					Output: partialOutput[line.PackageTest],
				}
			case "fail":
				ts.Failures++
				tc.Failure = &Failure{
					Output: partialOutput[line.PackageTest],
				}
			}
			ts.Tests++
			ts.TestCases = append(ts.TestCases, tc)
			delete(partialOutput, line.PackageTest)
		}
	}
	for line, output := range partialOutput {
		ts := testsuites.TestSuite(line.Package)
		ts.Tests++
		ts.Failures++
		ts.TestCases = append(ts.TestCases, TestCase{
			Name: line.Test,
			Failure: &Failure{
				Output: output,
			},
		})
	}
	output, err := xml.MarshalIndent(testsuites, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	os.Stdout.Write(output)
	os.Stdout.Write([]byte("\n"))
}
