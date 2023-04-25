// Copyright (C) 2023 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

// This file has the unit tests for the basic configurator functionality.
// Some functions are tested with isolated tests, and the full functionality
// is being tested with TestRun.
// The most cyclomatically complex functions have a fuzz test which varies the
// input yaml of the scheduler manifest.

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func testManifest() string {
	return createManifest("--config=/etc/kubernetes/scheduler-config-tas+gas.yaml")
}

func createManifest(configArg string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  creationTimestamp: null
  labels:
    component: kube-scheduler
    tier: control-plane
  name: kube-scheduler
  namespace: kube-system
spec:
  containers:
  - command:
    - kube-scheduler
    - %s
    - --authentication-kubeconfig=/etc/kubernetes/scheduler.conf
    - --authorization-kubeconfig=/etc/kubernetes/scheduler.conf
    - --bind-address=127.0.0.1
    - --kubeconfig=/etc/kubernetes/scheduler.conf
    - --leader-elect=true
    image: intel.com/custom/kube-scheduler-amd64:v1.25.2`, configArg)
}

func successCheckSetCommand(t *testing.T, exC *extenderConfigurator, description, configDestPath string) {
	t.Helper()

	yamlBytes, err := yaml.Marshal(exC.manifestData)
	if err != nil {
		t.Errorf("error %v for test %s", err, description)
	}

	yamlString := string(yamlBytes)
	if !strings.Contains(yamlString, "--config="+exC.configDestPath) {
		t.Errorf("command not found in test %s", description)
	}

	if strings.Count(yamlString, "-config") != 1 {
		t.Errorf("too many configs in test %s", description)
	}

	if strings.Count(yamlString, configDestPath) != 1 {
		t.Errorf("too many config dest paths in test %s", description)
	}
}

func hasExpectedError(err error, expectedPanicErrors ...error) bool {
	for _, expectedError := range expectedPanicErrors {
		if errors.Is(err, expectedError) {
			return true
		}
	}

	return false
}

func fuzzingHelper(f *testing.F, functionToTry string, expectedPanicErrors ...error) {
	f.Helper()

	manifest := testManifest()
	f.Add(manifest)

	f.Fuzz(func(t *testing.T, manifest string) {
		var panicErr error

		reachedFunctionToTry := false

		func() {
			defer panicHandler(&panicErr)

			var data map[string]interface{}

			// Unmarshal the YAML into the data map (this can panic for really bad yaml data)
			err := yaml.Unmarshal([]byte(manifest), &data)
			if err != nil {
				// overly bad format fuzzing, have to skip as we can't init the configurator with this
				return
			}

			configurator := createExtenderConfigurator()

			spec, ok := data["spec"]
			if ok {
				configurator.spec, _ = spec.(map[string]interface{})
			}

			reachedFunctionToTry = true

			reflect.ValueOf(configurator).MethodByName(functionToTry).Call([]reflect.Value{})
		}()

		if panicErr != nil && reachedFunctionToTry {
			if !hasExpectedError(panicErr, expectedPanicErrors...) {
				t.Errorf("unknown error %v", panicErr)
			}
		}
	})
}

func FuzzSetContainerVolumeMounts(f *testing.F) {
	fuzzingHelper(f, "SetContainerVolumeMounts", errNotFound, errTypeAssertion)
}

func FuzzSetCommand(f *testing.F) {
	fuzzingHelper(f, "SetCommand", errNotFound, errTypeAssertion)
}

func TestSetCommand(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		description    string
		manifest       string
		configDestPath string
		expectError    bool
	}

	testCases := []TestCase{
		{
			description: "empty manifest, hence no command",
			manifest:    "",
			expectError: true,
		},
		{
			description:    "normal",
			manifest:       createManifest("--config=foobar"),
			expectError:    false,
			configDestPath: "foobar",
		},
		{
			description:    "normal single dash",
			manifest:       createManifest("-config=foobar"),
			expectError:    false,
			configDestPath: "foobar",
		},
		{
			description:    "normal two-line",
			manifest:       createManifest("-config\n    - foobar"),
			expectError:    false,
			configDestPath: "foobar",
		},
	}

	var err error

	for _, testCase := range testCases {
		configurator := createExtenderConfigurator()
		configurator.configDestPath = testCase.configDestPath

		var data map[string]interface{}

		// Unmarshal the YAML into the data map
		err = yaml.Unmarshal([]byte(testCase.manifest), &data)

		if err != nil {
			t.Fatalf("problem in test:%v", err)
		}

		configurator.manifestData = data
		configurator.spec, _ = data["spec"].(map[string]interface{})

		func() {
			defer panicHandler(&err)

			configurator.SetCommand()
		}()

		if !testCase.expectError {
			if err != nil {
				t.Errorf("error %v for test %s", err, testCase.description)
			} else {
				// normal success case checks
				successCheckSetCommand(t, configurator, testCase.description, testCase.configDestPath)
			}
		}

		if testCase.expectError && err == nil {
			t.Errorf("expected error for test %s", testCase.description)
		}
	}
}

func TestGetPathData(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		inData      interface{}
		expected    interface{}
		description string
		path        []interface{}
		expectError bool
	}

	testCases := []TestCase{
		{
			description: "bad data",
			path:        []any{"Foo"},
			expected:    errTypeAssertion,
			expectError: true,
		},
		{
			description: "unknown path type",
			path:        []any{t},
			expected:    errUnknownType,
			expectError: true,
		},
		{
			description: "good data for string",
			path:        []any{"Foo"},
			inData:      map[string]interface{}{"Foo": "bar"},
			expected:    "bar",
		},
		{
			description: "good data for int",
			path:        []any{1},
			inData:      []interface{}{1, 2},
			expected:    2,
		},
	}

	for _, testCase := range testCases {
		result := getPathData(testCase.inData, testCase.path...)
		//nolint:forcetypeassert
		if result != testCase.expected && !(testCase.expectError && errors.Is(result.(error), testCase.expected.(error))) {
			t.Errorf("test %v result %v expected %v", testCase.description, result, testCase.expected)
		}
	}
}

func panicHandler(err *error) {
	if r := recover(); r != nil {
		if anErr, ok := r.(error); ok {
			*err = anErr
		}
	}
}

func TestReadSchedulerMinorVersion(t *testing.T) {
	t.Parallel()

	type TestCase struct {
		value       interface{}
		inData      map[string]interface{}
		description string
		expectError bool
	}

	testCases := []TestCase{
		{
			description: "bad data",
			inData:      nil,
			expectError: true,
		},
		{
			description: "bad version",
			inData: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"image": "bad version",
						},
					},
				},
			},
			expectError: true,
		},
		{
			description: "good version",
			inData: map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"image": ":v1.2.3",
						},
					},
				},
			},
			value: 2,
		},
	}

	for _, testCase := range testCases {
		var err error

		configurator := createExtenderConfigurator()
		configurator.manifestData = testCase.inData

		ver := 0

		func() {
			defer panicHandler(&err)

			ver = configurator.readSchedulerMinorVersion()
		}()

		if testCase.expectError {
			if err == nil {
				t.Errorf("expected error for test %s", testCase.description)
			}
		} else if ver != testCase.value {
			t.Errorf("result %v expected %v", ver, testCase.value)
		}
	}
}

func TestGetSchedulerAPIVersion1(t *testing.T) {
	t.Parallel()

	if _, err := getSchedulerAPIVersion(18); err == nil {
		t.Errorf("expected error for version 18")
	}

	if ver, err := getSchedulerAPIVersion(19); err != nil || ver != v1beta1 {
		t.Errorf("expected %q for version 19, got %q", v1beta1, ver)
	}

	if ver, err := getSchedulerAPIVersion(21); err != nil || ver != v1beta1 {
		t.Errorf("expected %q for version 21, got %q", v1beta1, ver)
	}
}

func TestGetSchedulerAPIVersion2(t *testing.T) {
	t.Parallel()

	if ver, err := getSchedulerAPIVersion(22); err != nil || ver != v1beta2 {
		t.Errorf("expected %q for version 22, got %q", v1beta2, ver)
	}

	if ver, err := getSchedulerAPIVersion(24); err != nil || ver != v1beta2 {
		t.Errorf("expected %q for version 24, got %q", v1beta2, ver)
	}

	if ver, err := getSchedulerAPIVersion(25); err != nil || ver != version1 {
		t.Errorf("expected %q for version 25, got %q", version1, ver)
	}
}

func TestCopyFiles(t *testing.T) {
	t.Parallel()

	tmpFolder, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("couldn't create tmp dir")
	}

	defer func() {
		os.RemoveAll(tmpFolder)
	}()

	srcFileName := filepath.Join(tmpFolder, "testfile.txt")
	testString := "testdata"
	err = os.WriteFile(srcFileName, []byte(testString), 0o600)

	if err != nil {
		t.Fatalf("couldn't create test file")
	}

	dstFileName := filepath.Join(tmpFolder, "copy.txt")

	copyFiles([]copyInfo{
		{
			inputFilePath:  srcFileName,
			outputFilePath: dstFileName,
			perm:           0o600,
		},
	})

	bytes, err := os.ReadFile(dstFileName)
	if err != nil {
		t.Errorf("dst file reading failed")
	}

	if testString != string(bytes) {
		t.Errorf("copy content differs")
	}
}

func fatalErrCheck(t *testing.T, err error, errorMessage string) {
	t.Helper()

	if err != nil {
		t.Fatalf(errorMessage)
	}
}

func fatalOkCheck(t *testing.T, ok bool, errorMessage string) {
	t.Helper()

	if !ok {
		t.Fatalf(errorMessage)
	}
}

func TestCopyCerts(t *testing.T) {
	t.Parallel()

	tmpFolder, err := os.MkdirTemp("", "test")

	fatalErrCheck(t, err, "couldn't create tmp dir")

	defer func() {
		os.RemoveAll(tmpFolder)
	}()

	type TestCase struct {
		srcFiles    map[string]string
		description string
		expectError bool
	}

	testCases := []TestCase{
		{
			srcFiles: map[string]string{
				"key": "foo",
				"crt": "crt",
			},
			expectError: false,
		},
		{
			srcFiles:    nil,
			expectError: true,
		},
	}

	configurator := createExtenderConfigurator()
	configurator.certDir = tmpFolder

	for _, testCase := range testCases {
		configurator.certCrtFile = filepath.Join(configurator.certDir, testCase.srcFiles["crt"])
		configurator.certKeyFile = filepath.Join(configurator.certDir, testCase.srcFiles["key"])

		for key, value := range testCase.srcFiles {
			err = os.WriteFile(filepath.Join(configurator.certDir, testCase.srcFiles[key]), []byte(value), 0o600)

			fatalErrCheck(t, err, "couldn't create test file")
		}

		var err error

		func() {
			defer panicHandler(&err)

			configurator.copyCerts()
		}()

		if testCase.expectError {
			if err == nil {
				t.Errorf("expected error for test %s", testCase.description)
			}
		} else {
			crtData, err := os.ReadFile(filepath.Join(tmpFolder, "client.crt"))
			keyData, err2 := os.ReadFile(filepath.Join(tmpFolder, "client.key"))
			if err != nil || err2 != nil {
				t.Errorf("cert copy failed")
			}

			if string(crtData) != testCase.srcFiles["crt"] ||
				string(keyData) != testCase.srcFiles["key"] {
				t.Errorf("copied cert doesn't match src")
			}
		}
	}
}

func TestCreateBackups(t *testing.T) {
	t.Parallel()

	tmpFolder, err := os.MkdirTemp("", "test")

	fatalErrCheck(t, err, "couldn't create tmp dir")

	defer func() {
		os.RemoveAll(tmpFolder)
	}()

	type TestCase struct {
		srcFiles    map[string]string
		description string
		expectError bool
	}

	manifestFilePath := filepath.Join(tmpFolder, "manifest")
	configFilePath := filepath.Join(tmpFolder, "config")
	testCases := []TestCase{
		{
			srcFiles: map[string]string{
				manifestFilePath: "foo",
				configFilePath:   "bar",
			},
			expectError: false,
		},
	}

	configurator := createExtenderConfigurator()
	configurator.backupDestination = tmpFolder
	configurator.manifestPath = manifestFilePath
	configurator.configDestPath = configFilePath

	for _, testCase := range testCases {
		for key, value := range testCase.srcFiles {
			err = os.WriteFile(key, []byte(value), 0o600)

			fatalErrCheck(t, err, "couldn't create test file")
		}

		var err error

		var backupFolder string

		func() {
			defer panicHandler(&err)

			backupFolder = configurator.createBackups()
		}()

		if testCase.expectError {
			if err == nil {
				t.Errorf("expected error for test %s", testCase.description)
			}
		} else {
			backupManifest, err := os.ReadFile(filepath.Join(backupFolder, "manifest"))
			backupConfig, err2 := os.ReadFile(filepath.Join(backupFolder, "config"))
			if err != nil || err2 != nil {
				t.Errorf("backup failed")
			}

			if string(backupManifest) != testCase.srcFiles[manifestFilePath] ||
				string(backupConfig) != testCase.srcFiles[configFilePath] {
				t.Errorf("backup doesn't match src")
			}
		}
	}
}

func successCheckTestRun(t *testing.T, configFilePath, manifestFilePath string) {
	t.Helper()

	schedYamlData, err := os.ReadFile(manifestFilePath)
	if err != nil {
		t.Fatalf("manifest not found from path %q, err:%v", manifestFilePath, err)
	}

	var data map[string]interface{}

	// Unmarshal the YAML into the data map
	err = yaml.Unmarshal(schedYamlData, &data)
	if err != nil {
		t.Fatalf("unmarshaling of %q failed, err: %v", manifestFilePath, err)
	}

	value, ok1 := getPathData(data, "spec", "dnsPolicy").(string)

	if !ok1 || value != "ClusterFirstWithHostNet" {
		t.Errorf("dnsPolicy not found from manifest")
	}

	volumeMounts, ok := getPathData(data, "spec", "containers", 0, "volumeMounts").([]interface{})

	fatalOkCheck(t, ok, "volumeMounts not found or wrong type")

	foundConfigMount := false
	foundCertMount := false

	for _, volumeMount := range volumeMounts {
		aMap, ok := volumeMount.(map[string]interface{})

		fatalOkCheck(t, ok, "unexpected volumeMount type")

		if reflect.DeepEqual(aMap, map[string]interface{}{
			"name":      schedulerConfigID,
			"mountPath": configFilePath,
			"readOnly":  true,
		}) {
			foundConfigMount = true
		}

		if reflect.DeepEqual(aMap, map[string]interface{}{
			"name":      certDirID,
			"mountPath": certVolumePath,
			"readOnly":  true,
		}) {
			foundCertMount = true
		}
	}

	if !foundConfigMount {
		t.Errorf("config mount not found")
	}

	if !foundCertMount {
		t.Errorf("cert mount not found")
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	tmpFolder, err := os.MkdirTemp("", "test")
	if err != nil {
		t.Fatalf("couldn't create tmp dir")
	}

	defer func() {
		os.RemoveAll(tmpFolder)
	}()

	type TestCase struct {
		srcFiles    map[string]string
		description string
		args        []string
		expectError bool
		dryRun      bool
	}

	manifestFilePath := filepath.Join(tmpFolder, "manifest")
	configFilePath := filepath.Join(tmpFolder, "config")
	crtFilePath := filepath.Join(tmpFolder, "crt")
	keyFilePath := filepath.Join(tmpFolder, "key")
	testCases := []TestCase{
		{
			srcFiles:    map[string]string{},
			expectError: true,
			args:        []string{},
		},
		{
			srcFiles: map[string]string{
				manifestFilePath: testManifest(),
				configFilePath:   "bar",
				crtFilePath:      "crtdata",
				keyFilePath:      "keydata",
			},
			expectError: false,
			args: []string{
				"-f", configFilePath,
				"-m", manifestFilePath,
				"-b", tmpFolder,
				"-d", tmpFolder,
				"-cert", crtFilePath,
				"-key", keyFilePath,
				"-cert-dir", tmpFolder,
			},
		},
		{
			srcFiles: map[string]string{
				manifestFilePath: testManifest(),
				configFilePath:   "bar",
				crtFilePath:      "crtdata",
				keyFilePath:      "keydata",
			},
			expectError: false,
			args: []string{
				"-f", configFilePath,
				"-m", manifestFilePath,
				"-b", tmpFolder,
				"-d", tmpFolder,
				"-cert", crtFilePath,
				"-key", keyFilePath,
				"-cert-dir", tmpFolder,
				"-dry-run",
			},
			dryRun: true,
		},
	}

	for _, testCase := range testCases {
		for key, value := range testCase.srcFiles {
			err = os.WriteFile(key, []byte(value), 0o600)
			if err != nil {
				t.Fatalf("couldn't create test file")
			}
		}

		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		origArgs := os.Args
		os.Args = append([]string{os.Args[0]}, testCase.args...)
		err = run()

		os.Args = origArgs

		if !testCase.expectError {
			if err != nil {
				t.Errorf("error %v for test %s", err, testCase.description)
			} else if !testCase.dryRun {
				// normal success case checks
				successCheckTestRun(t, configFilePath, manifestFilePath)
			}
		}

		if testCase.expectError && err == nil {
			t.Errorf("expected error for test %s", testCase.description)
		}
	}
}
