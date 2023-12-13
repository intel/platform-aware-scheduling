// Copyright (C) 2023 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
)

//nolint:gochecknoglobals
var (
	goVersion = "value is set during build"
	buildDate = "value is set during build"
	version   = "value is set during build"
)

type extenderConfigurator struct {
	manifestData      map[string]interface{}
	spec              map[string]interface{}
	backupDestination string
	certCrtFile       string
	certDir           string
	certKeyFile       string
	configDestination string
	configDestPath    string
	configPath        string
	deploymentPathGAS string
	deploymentPathTAS string
	manifestPath      string
	origManifest      string
	dryRun            bool
}

type copyInfo struct {
	inputFilePath  string
	outputFilePath string
	perm           fs.FileMode
}

const (
	schedulerConfigID = "schedulerconfig"
	certDirID         = "certdir"
	v1beta1           = "v1beta1"
	v1beta2           = "v1beta2"
	version1          = "v1"
	certVolumePath    = "/host/certs"
	perm0666          = 0o666
	perm0644          = 0o644
	perm0600          = 0o600
	perm0755          = 0o755
	v19               = 19
	v22               = 22
	v24               = 24
	v25               = 25
	imageNameMatches  = 4
)

var (
	errIndexingError = errors.New("array indexing error")
	errTypeAssertion = errors.New("type assertion error")
	errUnknownType   = errors.New("unknown type error")
	errNotFound      = errors.New("not found error")
	errNotSupported  = errors.New("not supported error")
	errFileChecks    = errors.New("file check error")
)

// getPathData finds a path in parsed yaml data and returns the interface at the
// end of the path.
func getPathData(data interface{}, path ...interface{}) interface{} {
	for _, anInterface := range path {
		switch reflect.TypeOf(anInterface).String() {
		case "int":
			//nolint:forcetypeassert
			value := anInterface.(int)
			anArray, ok := data.([]interface{})

			if !ok || value >= len(anArray) || value < 0 {
				return fmt.Errorf("%w: array len %v bad index %v", errIndexingError, len(anArray), value)
			}

			data = anArray[value]
		case "string":
			aMap, ok := data.(map[string]interface{})
			if !ok {
				return fmt.Errorf("%w: not a map:%v", errTypeAssertion, aMap)
			}

			//nolint:forcetypeassert
			value := anInterface.(string)
			data = aMap[value]

			if data == nil {
				return fmt.Errorf("%w: bad map key %v", errNotFound, value)
			}
		default:
			return fmt.Errorf("%w: unknown type %q", errUnknownType, reflect.TypeOf(anInterface).String())
		}
	}

	return data
}

// readSchedulerMinorVersion tries to return the minor version as int by going
// through the parsed yaml data structure.
func (ec *extenderConfigurator) readSchedulerMinorVersion() int {
	imageName, ok := getPathData(ec.manifestData, "spec", "containers", 0, "image").(string)
	if !ok {
		panic(fmt.Errorf("%w: imageName not found", errNotFound))
	}

	compiledRegexp := regexp.MustCompile(`^.*:v(\d+\.)?(\d+\.)?(\*|\d+).*$`)

	matches := compiledRegexp.FindStringSubmatch(imageName)
	if len(matches) != imageNameMatches {
		panic(fmt.Errorf("%w: version not found from imageName: %s", errNotFound, imageName))
	}

	ver, err := strconv.Atoi(strings.TrimSuffix(compiledRegexp.FindStringSubmatch(imageName)[2], "."))
	if err != nil {
		panic(fmt.Errorf("%w: couldn't convert version number %s", err, compiledRegexp.FindStringSubmatch(imageName)[2]))
	}

	return ver
}

func getSchedulerAPIVersion(schedulerVer int) (string, error) {
	switch {
	case schedulerVer < v19:
		return "", fmt.Errorf("%w: k8s version older than 1.19 isn't supported", errNotSupported)
	case schedulerVer < v22:
		return v1beta1, nil

	case schedulerVer < v25:
		return v1beta2, nil
	}

	return version1, nil
}

func checkFileAccess(filepath string, flag int, perm fs.FileMode, tryCreate bool) error {
	file, err := os.OpenFile(filepath, flag, perm)
	if err != nil && tryCreate {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no access to the file at path %q, err: %w", filepath, err)
		}

		// fine, try create
		file2, err2 := os.Create(filepath)
		if err2 != nil {
			return fmt.Errorf("user can't create the file at path %q, err: %w", filepath, err)
		}

		file2.Close()

		err2 = os.Remove(filepath)
		if err2 != nil {
			return fmt.Errorf("surprisingly, removing of the newly created file failed, err: %w", err)
		}

		// creation successful
		return nil
	} else if err != nil {
		return fmt.Errorf("no access to the file at path %q, err: %w", filepath, err)
	}

	file.Close()

	return nil
}

type checkedFile struct {
	path      string
	flag      int
	perm      fs.FileMode
	tryCreate bool
}

// checkFileAccesses has some file checks in order to fail early if user doesn't have
// access to some of the files.
//
//nolint:nosnakecase
func (ec *extenderConfigurator) checkFileAccesses() {
	errLog := ""

	destPath := filepath.Join(ec.configDestination, filepath.Base(ec.configPath))
	ec.configDestPath = destPath

	checkedFiles := []checkedFile{
		{
			path:      ec.manifestPath,
			flag:      os.O_RDWR,
			perm:      perm0666,
			tryCreate: false,
		},
		{
			path:      ec.configPath,
			flag:      os.O_RDONLY,
			perm:      perm0666,
			tryCreate: false,
		},
		{
			path:      destPath,
			flag:      os.O_RDWR,
			perm:      perm0644,
			tryCreate: true,
		},
		{
			path:      ec.certKeyFile,
			flag:      os.O_RDONLY,
			perm:      perm0600,
			tryCreate: false,
		},
		{
			path:      ec.certCrtFile,
			flag:      os.O_RDONLY,
			perm:      perm0644,
			tryCreate: false,
		},
	}

	nlAfterFirstLine := ""

	for _, file := range checkedFiles {
		err := checkFileAccess(file.path, file.flag, file.perm, file.tryCreate)
		if err != nil {
			errLog += nlAfterFirstLine + err.Error()
			nlAfterFirstLine = "\n"
		}
	}

	err := unix.Access(ec.certDir, unix.W_OK)
	if err != nil && errors.Is(err, syscall.ENOENT) {
		err = os.MkdirAll(ec.certDir, perm0755)

		if err != nil {
			errLog += nlAfterFirstLine + err.Error()
		} else {
			os.RemoveAll(ec.certDir)
		}
	} else if err != nil {
		errLog += nlAfterFirstLine + ec.certDir + ": " + err.Error()
	}

	if errLog != "" {
		panic(fmt.Errorf("%w: %s", errFileChecks, errLog))
	}
}

// copyFiles is a utility function for copying a number of files.
func copyFiles(copyFiles []copyInfo) {
	for _, copyFile := range copyFiles {
		input, err := os.ReadFile(copyFile.inputFilePath)
		if err != nil {
			panic(fmt.Errorf("error reading file %q, err:%w", copyFile.inputFilePath, err))
		}

		err = os.WriteFile(copyFile.outputFilePath, input, copyFile.perm)
		if err != nil {
			panic(fmt.Errorf("error writing file %q to %q, err:%w",
				copyFile.inputFilePath, copyFile.outputFilePath, err))
		}
	}
}

// copyCerts copies the cert files to the chosen destination folder (usually /etc/certs/).
func (ec *extenderConfigurator) copyCerts() {
	err := os.MkdirAll(ec.certDir, perm0755)
	if err != nil {
		panic(fmt.Errorf("couldn't create certdir at %q, err: %w", ec.certDir, err))
	}

	copyInfos := []copyInfo{
		{
			inputFilePath:  ec.certKeyFile,
			outputFilePath: filepath.Join(ec.certDir, "client.key"),
			perm:           perm0600,
		},
		{
			inputFilePath:  ec.certCrtFile,
			outputFilePath: filepath.Join(ec.certDir, "client.crt"),
			perm:           perm0644,
		},
	}

	copyFiles(copyInfos)

	klog.Infof("Certs written to folder %q", ec.certDir)
}

// createBackups creates a backup of Manifest and the config destination file to a
// temporary folder with a prefix "backup_".
func (ec *extenderConfigurator) createBackups() string {
	var err error

	var backupFolder string

	if ec.backupDestination != "/dev/null" {
		backupFolder, err = os.MkdirTemp(ec.backupDestination, "backup_"+time.Now().Format("2006-01-02")+"_")
		if err != nil {
			panic(fmt.Errorf("user can't create folders to %q, err: %w", ec.backupDestination, err))
		}

		copyInfos := []copyInfo{
			{
				inputFilePath:  ec.manifestPath,
				outputFilePath: filepath.Join(backupFolder, filepath.Base(ec.manifestPath)),
				perm:           perm0644,
			},
		}

		if _, err := os.Stat(ec.configDestPath); err == nil {
			copyInfos = append(copyInfos, copyInfo{
				inputFilePath:  ec.configDestPath,
				outputFilePath: filepath.Join(backupFolder, filepath.Base(ec.configDestPath)),
				perm:           perm0644,
			})
		}

		copyFiles(copyInfos)

		klog.Infof("Backups written to folder %q", backupFolder)
	}

	return backupFolder
}

// readManifest reads the manifest file, parses it as yaml and stores it as a map.
func (ec *extenderConfigurator) readManifest() {
	schedYamlData, err := os.ReadFile(ec.manifestPath)
	if err != nil {
		panic(fmt.Errorf("manifest not found from path %q, err:%w", ec.manifestPath, err))
	}

	ec.origManifest = string(schedYamlData)

	var data map[string]interface{}

	// Unmarshal the YAML into the data map
	err = yaml.Unmarshal([]byte(ec.origManifest), &data)
	if err != nil {
		panic(fmt.Errorf("unmarshaling of %q failed, err: %w", ec.manifestPath, err))
	}

	ec.manifestData = data
	spec, ok := data["spec"].(map[string]interface{})

	if !ok {
		panic(fmt.Errorf("%w: spec not found", errNotFound))
	}

	ec.spec = spec
}

func (ec *extenderConfigurator) setVolumes() {
	volumes, ok := ec.spec["volumes"].([]interface{})
	if !ok {
		volumes = []interface{}{}
		ec.spec["volumes"] = volumes
	}

	newVolumes := []map[string]interface{}{}

	for _, aMapIf := range volumes {
		aMap, ok1 := aMapIf.(map[string]interface{})
		if !ok1 {
			panic(fmt.Errorf("%w: volume type error in manifest file, should have been a map", errTypeAssertion))
		}

		name, ok2 := aMap["name"].(string)
		if !ok2 {
			klog.Errorf("found volume without a name in manifest file, dropping it")

			continue
		}

		if !(name == certDirID) &&
			!(name == schedulerConfigID) {
			newVolumes = append(newVolumes, aMap)
		}
	}

	newVolumes = append(newVolumes, map[string]interface{}{
		"name":     schedulerConfigID,
		"hostPath": map[string]interface{}{"path": ec.configDestPath},
	})

	newVolumes = append(newVolumes, map[string]interface{}{
		"name":     certDirID,
		"hostPath": map[string]interface{}{"path": ec.certDir},
	})

	ec.spec["volumes"] = newVolumes
}

// SetContainerVolumeMounts sets the container volume mounts. Public for testability.
func (ec *extenderConfigurator) SetContainerVolumeMounts() {
	container, ok1 := getPathData(ec.spec, "containers", 0).(map[string]interface{})
	if !ok1 {
		panic(fmt.Errorf("%w: containers not found in manifest file", errNotFound))
	}

	volumeMounts, ok2 := container["volumeMounts"]
	if !ok2 {
		volumeMounts = []interface{}{}
		container["volumeMounts"] = volumeMounts
	}

	newVolumeMounts := []map[string]interface{}{}
	mountMaps, ok3 := volumeMounts.([]interface{})

	if !ok3 {
		panic(fmt.Errorf("%w: volumeMounts type wrong, should have been array in manifest file", errTypeAssertion))
	}

	for _, aMapIf := range mountMaps {
		aMap, ok4 := aMapIf.(map[string]interface{})
		if !ok4 {
			panic(fmt.Errorf("%w: volumeMount type wrong, should have been a map in manifest file", errTypeAssertion))
		}

		name, ok5 := aMap["name"].(string)
		if !ok5 {
			klog.Errorf("found volume mount without a name in manifest file, dropping it")

			continue
		}

		if !(name == schedulerConfigID) &&
			!(name == certDirID) {
			newVolumeMounts = append(newVolumeMounts, aMap)
		}
	}

	newVolumeMounts = append(newVolumeMounts, map[string]interface{}{
		"name":      schedulerConfigID,
		"mountPath": ec.configDestPath,
		"readOnly":  true,
	})

	newVolumeMounts = append(newVolumeMounts, map[string]interface{}{
		"name":      certDirID,
		"mountPath": certVolumePath,
		"readOnly":  true,
	})

	container["volumeMounts"] = newVolumeMounts
}

func panicOkCheck(ok bool, msg string, err error) {
	if !ok {
		klog.Errorf(msg)
		panic(err)
	}
}

// skipArg returns whether the arg should be dropped when constructing
// the new command line. The latter bool will tell if also the next arg
// should be dropped (command syntax without '=').
func skipArg(arg string) (bool, bool) {
	for _, argToDrop := range []string{"config", "policy-configmap", "policy-configmap-namespace"} {
		for _, prefix := range []string{"-", "--"} {
			if arg == prefix+argToDrop {
				return true, true
			}

			if strings.HasPrefix(arg, prefix+argToDrop+"=") {
				return true, false
			}
		}
	}

	return false, false
}

// SetCommand sets the command to the container. Public for testability.
func (ec *extenderConfigurator) SetCommand() {
	// find command array
	container, ok1 := getPathData(ec.spec, "containers", 0).(map[string]interface{})
	panicOkCheck(ok1, "containers not found in manifest file", errNotFound)

	command, ok2 := container["command"]
	panicOkCheck(ok2, "'command' missing from YAML spec container in manifest file", errNotFound)

	// create new command array without old settings
	newCommands := []string{}

	commands, ok3 := command.([]interface{})
	panicOkCheck(ok3, "command is not an array type in manifest file", errTypeAssertion)

	skipNext := false

	var skip bool

	for _, cmdif := range commands {
		if skipNext {
			skipNext = false

			continue
		}

		cmd, ok4 := cmdif.(string)
		panicOkCheck(ok4, "command array item is not a string type in manifest file", errTypeAssertion)

		if skip, skipNext = skipArg(cmd); skip {
			continue
		}

		newCommands = append(newCommands, cmd)
	}

	newCommands = append(newCommands, "--config="+ec.configDestPath)

	// finally, store modified commands
	container["command"] = newCommands
}

func numLevenshteinChanges(diffs []diffmatchpatch.Diff) int {
	dmp := diffmatchpatch.New()

	return dmp.DiffLevenshtein(diffs)
}

func printDiffs(diffs []diffmatchpatch.Diff) {
	dmp := diffmatchpatch.New()

	if dmp.DiffLevenshtein(diffs) == 0 {
		klog.Info("no changes")
	} else {
		klog.Infof("\n" + dmp.DiffPrettyText(diffs))
	}
}

func (ec *extenderConfigurator) createConfig(minorVersion int, numChanges *int) {
	apiVersion, err := getSchedulerAPIVersion(minorVersion)
	if err != nil {
		panic(fmt.Errorf("error fetching scheduler API version: %w", err))
	}

	read, err := os.ReadFile(ec.configPath)
	if err != nil {
		panic(fmt.Errorf("couldn't read config file %q, err: %w", ec.configPath, err))
	}

	modified := strings.Replace(string(read), "XVERSIONX", apiVersion, 1)

	dmp := diffmatchpatch.New()

	origConfigData, err := os.ReadFile(ec.configDestPath)
	if err != nil {
		origConfigData = []byte("")
	}

	diffs := dmp.DiffMain(string(origConfigData), modified, false)

	if ec.dryRun {
		klog.Infof("Scheduler config changes compared to %q:\n", ec.configDestPath)

		printDiffs(diffs)

		return
	}

	numNewChanges := numLevenshteinChanges(diffs)
	*numChanges += numNewChanges

	if numNewChanges > 0 {
		err = os.WriteFile(ec.configDestPath, []byte(modified), perm0644)
		if err != nil {
			panic(fmt.Errorf("config %q writing failed with err %w", ec.configDestPath, err))
		}

		klog.Infof("Scheduler config written to %q", ec.configDestPath)
	} else {
		klog.Infof("No changes needed, config not written.")
	}
}

// modifyDeployments changes the deployment file(s) for the users of k8s version 1.23 or older.
func (ec *extenderConfigurator) modifyDeployments(minorVersion int) {
	if minorVersion >= v24 {
		return
	}

	for _, deployment := range []string{ec.deploymentPathTAS, ec.deploymentPathGAS} {
		if deployment == "" {
			continue
		}

		read, err := os.ReadFile(deployment)
		if err != nil {
			panic(fmt.Errorf("couldn't read deployment file %q, err: %w", deployment, err))
		}

		modified := strings.ReplaceAll(string(read), "control-plane", "master")

		if ec.dryRun {
			klog.Infof("Deployment at %q changes:", deployment)

			dmp := diffmatchpatch.New()
			printDiffs(dmp.DiffMain(string(read), modified, false))
		} else {
			err = os.WriteFile(deployment, []byte(modified), perm0644)
			if err != nil {
				panic(fmt.Errorf("deployment %q yaml writing failed with err: %w", deployment, err))
			}

			klog.Infof("Deployment at %q modified", deployment)
		}
	}
}

// writeManifest marshals the given manifest data map to proper yaml format and overwrites
// the manifest yaml file with it.
func (ec *extenderConfigurator) writeManifest(numChanges *int) {
	bytes, err := yaml.Marshal(ec.manifestData)
	if err != nil {
		panic(fmt.Errorf("marshaling failed: %w", err))
	}

	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(ec.origManifest, string(bytes), false)

	if ec.dryRun {
		klog.Infof("Manifest changes at %q:", ec.manifestPath)
		printDiffs(diffs)

		return
	}

	numNewChanges := numLevenshteinChanges(diffs)
	*numChanges += numNewChanges

	if numNewChanges > 0 {
		err = os.WriteFile(ec.manifestPath, bytes, perm0644)
		if err != nil {
			panic(fmt.Errorf("manifest %q yaml writing failed with err: %w", ec.manifestPath, err))
		}

		klog.Infof("Manifest written to %q", ec.manifestPath)
	} else {
		klog.Infof("No changes needed, manifest not written.")
	}
}

func (ec *extenderConfigurator) printFileLocations() {
	klog.Infof("Manifest file is located at: %q", ec.manifestPath)
	klog.Infof("Scheduler config file is located at: %q", ec.configPath)
	klog.Infof("Scheduler config destination will be: %q", ec.configDestPath)
}

func createExtenderConfigurator() *extenderConfigurator {
	return &extenderConfigurator{
		manifestData:      map[string]interface{}{},
		spec:              map[string]interface{}{},
		backupDestination: "",
		certCrtFile:       "",
		certDir:           certDirID,
		certKeyFile:       "",
		configDestination: "",
		configDestPath:    "",
		configPath:        "",
		deploymentPathGAS: "",
		deploymentPathTAS: "",
		manifestPath:      "",
		origManifest:      "",
		dryRun:            false,
	}
}

func defineFlags(configurator *extenderConfigurator) {
	flag.StringVar(&configurator.backupDestination, "b", "/etc/kubernetes",
		"Specify the folder where backup- prefixed backup folders are created.\nUse /dev/null if you don't want backups.")

	flag.StringVar(&configurator.certCrtFile, "cert", "/etc/kubernetes/pki/ca.crt",
		"Specify the path to the certificate file.")

	flag.StringVar(&configurator.certDir, "cert-dir", "/etc/certs/", "Specify the destination dir for the certificate.")

	flag.StringVar(&configurator.configDestination, "d", "/etc/kubernetes",
		"Specify the destination folder for the kube scheduler config file.\nRequired only from K8s v22 onwards.")

	flag.BoolVar(&configurator.dryRun, "dry-run", false,
		"Trial run with diff output for scheduler settings, but no real changes.")

	flag.StringVar(&configurator.configPath, "f", "deploy/extender-configuration/scheduler-config.yaml",
		"Specify the path to the kube scheduler configuration file.\nRequired only from K8s v22 onwards.")

	flag.StringVar(&configurator.deploymentPathGAS, "gas-depl", "",
		"Specify the path to the GAS scheduler extender deployment file.\nRequired only for K8s v23 or older.")

	flag.StringVar(&configurator.certKeyFile, "key", "/etc/kubernetes/pki/ca.key",
		"Specify the path to the certificate key file.")

	flag.StringVar(&configurator.manifestPath, "m", "/etc/kubernetes/manifests/kube-scheduler.yaml",
		"Specify the path to the Kubernetes manifest kube-scheduler.yaml file")

	flag.StringVar(&configurator.deploymentPathTAS, "tas-depl", "deploy/tas-deployment.yaml",
		"Specify the path to the TAS scheduler extender deployment file.\nRequired only for K8s v23 or older.")

	flag.Parse()
}

func run() (err error) {
	klog.Infof("%s built on %s with go %s", version, buildDate, goVersion)

	configurator := createExtenderConfigurator()

	defineFlags(configurator)

	defer func() {
		if r := recover(); r != nil {
			if anErr, ok := r.(error); ok {
				err = anErr
			}
		}
	}()

	numChanges := 0
	backupFolder := ""

	// in error situations execution bails with panic from the functions below
	configurator.checkFileAccesses()
	configurator.readManifest()
	minorVer := configurator.readSchedulerMinorVersion()

	if !configurator.dryRun {
		backupFolder = configurator.createBackups()
		configurator.copyCerts()
	}

	configurator.printFileLocations()
	configurator.SetCommand()
	configurator.SetContainerVolumeMounts()
	configurator.setVolumes()
	configurator.createConfig(minorVer, &numChanges)
	configurator.modifyDeployments(minorVer)
	configurator.spec["dnsPolicy"] = "ClusterFirstWithHostNet"
	configurator.writeManifest(&numChanges)

	if !configurator.dryRun && numChanges == 0 && backupFolder != "" {
		os.RemoveAll(backupFolder)
		klog.Infof("No changes, backup folder %v removed.", backupFolder)
	}

	klog.Info("Done.")

	return err // err is set in the deferred panic recovery func
}

func main() {
	if err := run(); err != nil {
		klog.Info(err)
	}
}
