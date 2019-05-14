package testing

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	th "github.com/gophercloud/gophercloud/testhelper"
	"github.com/gophercloud/utils/openstack/baremetal/v1/nodes"
)

func TestUserDataFromMap(t *testing.T) {
	userData, err := IgnitionUserData.ToUserData()
	th.AssertNoErr(t, err)
	th.CheckJSONEquals(t, string(userData), IgnitionUserData)
}

func TestUserDataFromString(t *testing.T) {
	cloudInit := nodes.UserDataString(CloudInitString)
	userData, err := cloudInit.ToUserData()
	th.AssertNoErr(t, err)
	th.AssertByteArrayEquals(t, userData, []byte(cloudInit))
}

func TestConfigDriveToDirectory(t *testing.T) {
	path, err := ConfigDrive.ToDirectory()
	th.AssertNoErr(t, err)
	defer os.RemoveAll(path)

	basePath := filepath.FromSlash(path + "/openstack/latest")

	userData, err := ioutil.ReadFile(filepath.FromSlash(basePath + "/user_data"))
	th.AssertNoErr(t, err)
	th.CheckJSONEquals(t, string(userData), IgnitionUserData)

	metaData, err := ioutil.ReadFile(filepath.FromSlash(basePath + "/meta_data.json"))
	th.AssertNoErr(t, err)
	th.CheckJSONEquals(t, string(metaData), OpenStackMetaData)

	networkData, err := ioutil.ReadFile(filepath.FromSlash(basePath + "/network_data.json"))
	th.AssertNoErr(t, err)
	th.CheckJSONEquals(t, string(networkData), NetworkData)
}

func TestConfigDriveVersionToDirectory(t *testing.T) {
	path, err := ConfigDriveVersioned.ToDirectory()
	th.AssertNoErr(t, err)
	defer os.RemoveAll(path)

	basePath := filepath.FromSlash(path + "/openstack/" + ConfigDriveVersioned.Version)

	userData, err := ioutil.ReadFile(filepath.FromSlash(basePath + "/user_data"))
	th.AssertNoErr(t, err)
	th.CheckJSONEquals(t, string(userData), IgnitionUserData)

	metaData, err := ioutil.ReadFile(filepath.FromSlash(basePath + "/meta_data.json"))
	th.AssertNoErr(t, err)
	th.CheckJSONEquals(t, string(metaData), OpenStackMetaData)

	networkData, err := ioutil.ReadFile(filepath.FromSlash(basePath + "/network_data.json"))
	th.AssertNoErr(t, err)
	th.CheckJSONEquals(t, string(networkData), NetworkData)
}
