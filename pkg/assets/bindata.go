// Code generated for package assets by go-bindata DO NOT EDIT. (@generated)
// sources:
// bindata/nodecadaemon.yaml
package assets

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _nodecadaemonYaml = []byte(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: node-ca
  namespace: openshift-image-registry
spec:
  selector:
    matchLabels:
      name: node-ca
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 10%
  template:
    metadata:
      annotations:
        target.workload.openshift.io/management: '{"effect": "PreferredDuringScheduling"}'
      labels:
        name: node-ca
    spec:
      nodeSelector:
        kubernetes.io/os: linux
      priorityClassName: system-cluster-critical
      tolerations:
      - operator: Exists
      hostNetwork: true # run as host network to tolerate unready networks
      serviceAccountName: node-ca
      containers:
      - name: node-ca
        securityContext:
          privileged: true
          runAsUser: 1001
          runAsGroup: 0
        image: docker.io/openshift/origin-cluster-image-registry-operator:latest
        resources:
          requests:
            cpu: 10m
            memory: 10Mi
        command:
        - "/bin/sh"
        - "-c"
        - |
          trap 'jobs -p | xargs -r kill; echo shutting down node-ca; exit 0' TERM
          while [ true ];
          do
            for f in $(ls /tmp/serviceca); do
                echo $f
                ca_file_path="/tmp/serviceca/${f}"
                f=$(echo $f | sed  -r 's/(.*)\.\./\1:/')
                reg_dir_path="/etc/docker/certs.d/${f}"
                if [ -e "${reg_dir_path}" ]; then
                    cp -u $ca_file_path $reg_dir_path/ca.crt
                else
                    mkdir $reg_dir_path
                    cp $ca_file_path $reg_dir_path/ca.crt
                fi
            done
            for d in $(ls /etc/docker/certs.d); do
                echo $d
                dp=$(echo $d | sed  -r 's/(.*):/\1\.\./')
                reg_conf_path="/tmp/serviceca/${dp}"
                if [ ! -e "${reg_conf_path}" ]; then
                    rm -rf /etc/docker/certs.d/$d
                fi
            done
            sleep 60 & wait ${!}
          done
        volumeMounts:
        - name: serviceca
          mountPath: /tmp/serviceca
        - name: host
          mountPath: /etc/docker/certs.d
      volumes:
      - name: host
        hostPath:
          path: /etc/docker/certs.d
      - name: serviceca
        configMap:
          name: image-registry-certificates
`)

func nodecadaemonYamlBytes() ([]byte, error) {
	return _nodecadaemonYaml, nil
}

func nodecadaemonYaml() (*asset, error) {
	bytes, err := nodecadaemonYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "nodecadaemon.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"nodecadaemon.yaml": nodecadaemonYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"nodecadaemon.yaml": {nodecadaemonYaml, map[string]*bintree{}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
