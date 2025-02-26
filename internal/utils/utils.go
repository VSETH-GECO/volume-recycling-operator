/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"errors"
	"io/ioutil"
	"k8s.io/klog/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type FCUtil interface {
	WipeDisk(path string, filesystem string) error
	FindDisk(wwn, lun string) (string, string)
}

type LinuxFCUtil struct {
	FCUtil
}

const (
	byPath = "/hostdev/disk/by-path/"
)

// The following two functions were lifted from https://github.com/kubernetes/kubernetes/blob/master/pkg/volume/util/hostdevice_util_linux.go
// Kubernetes does not expose these for inclusion via import, so I had to unfortunately copy this over

// findDeviceForPath Find the underlying disk for a linked path such as /hostdev/disk/by-path/XXXX or /hostdev/mapper/XXXX
// will return sdX or hdX etc, if /hostdev/sdX is passed in then sdX will be returned
func (fUtil *LinuxFCUtil) findDeviceForPath(path string) (string, error) {
	devicePath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	// if path /hostdev/hdX split into "", "dev", "hdX" then we will
	// return just the last part
	parts := strings.Split(devicePath, "/")
	if len(parts) == 3 && strings.HasPrefix(parts[1], "hostdev") {
		return parts[2], nil
	}
	return "", errors.New("Illegal path for device " + devicePath)
}

// FindMultipathDeviceForDevice given a device name like /hostdev/sdx, find the devicemapper parent. If called with a device
// already resolved to devicemapper, do nothing.
func (fUtil *LinuxFCUtil) findMultipathDeviceForDevice(device string) string {
	if strings.HasPrefix(device, "/hostdev/dm-") {
		return device
	}
	disk, err := fUtil.findDeviceForPath(device)
	if err != nil {
		return ""
	}
	sysPath := "/sys/block/"
	if dirs, err := ioutil.ReadDir(sysPath); err == nil {
		for _, f := range dirs {
			name := f.Name()
			if strings.HasPrefix(name, "dm-") {
				if _, err1 := os.Lstat(sysPath + name + "/slaves/" + disk); err1 == nil {
					return "/hostdev/" + name
				}
			}
		}
	}
	return ""
}

// This is taken from https://github.com/kubernetes/kubernetes/blob/master/pkg/volume/fc/fc_util.go
// - this is also not exposed and therefore copied

// given a wwn and lun, find the device and associated devicemapper parent
func (fUtil *LinuxFCUtil) FindDisk(wwn, lun string) (string, string) {
	fcPathExp := "^(pci-.*-fc|fc)-0x" + wwn + "-lun-" + lun + "$"
	r := regexp.MustCompile(fcPathExp)
	devPath := byPath
	if dirs, err := ioutil.ReadDir(devPath); err == nil {
		for _, f := range dirs {
			name := f.Name()
			if r.MatchString(name) {
				if disk, err1 := filepath.EvalSymlinks(devPath + name); err1 == nil {
					dm := fUtil.findMultipathDeviceForDevice(disk)
					klog.Infof("fc: find disk: %v, dm: %v, fc path: %v", disk, dm, name)
					return disk, dm
				}
			}
		}
	}
	return "", ""
}

// WipeDisk is a custom helper function to actually wipe the disk and create a new filesystem on it
func (fUtil *LinuxFCUtil) WipeDisk(path string, filesystem string) error {
	cmd := exec.Command("wipefs", "-a", path)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	klog.Infof("wipeDisk: wipefs: %v", string(out))
	cmd = exec.Command("mkfs."+filesystem, path)
	out, err = cmd.Output()
	if err != nil {
		return err
	}
	klog.Infof("wipeDisk: mkfs: %v", string(out))
	return nil
}
