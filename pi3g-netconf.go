package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

var version = "0.2.6"

const (
	confPath = "/etc/tor/torrc"
	lockPath = "/tmp/pi3g-netconf-lock"
)

const (
	serviceBin = "/usr/sbin/service"
	ipBin      = "/bin/ip"
)

type Action int

const (
	NONE Action = iota
	START
	STOP
)

// restart a service (e.g. hostapd, tor)
func restartService(service string) error {
	out, err := exec.Command(serviceBin, service, "restart").CombinedOutput()
	debugf("restarting %s:\n%s", service, out)
	return err
}

// stop a service (e.g. hostapd, tor)
func stopService(service string) error {
	out, err := exec.Command(serviceBin, service, "stop").CombinedOutput()
	debugf("stop %s:\n%s", service, out)
	return err
}

// start a service (e.g. hostapd, tor)
func startService(service string) error {
	out, err := exec.Command(serviceBin, service, "start").CombinedOutput()
	debugf("starting %s:\n%s", service, out)
	return err
}

// add an ip address to an interface
func ipAddr(iface, subnet string) error {
	out, err := exec.Command(ipBin, "addr", "add", "192.168."+subnet+".1/24", "broadcast", "192.168."+subnet+".255", "dev", iface).CombinedOutput()
	debugf("setting ip on %s(%s):\n%s", iface, subnet, out)
	return err
}

// remove all ip addresses from an interface
func ipFlush(iface string) error {
	out, err := exec.Command(ipBin, "addr", "flush", "dev", iface).CombinedOutput()
	debugf("flushing addresses of %s:\n%s", iface, out)
	return err
}

// bring interface up
func ipUp(iface string) error {
	out, err := exec.Command(ipBin, "link", "set", "up", iface).CombinedOutput()
	debugf("bringing %s up:\n%s", iface, out)
	return err
}

// bring interface down
func ipDown(iface string) error {
	out, err := exec.Command(ipBin, "link", "set", "down", iface).CombinedOutput()
	debugf("bringing %s down:\n%s", iface, out)
	return err
}

func read() (string, error) {
	f, err := os.Open(confPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	c, err := ioutil.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(c), nil
}

func write(s string) error {
	return ioutil.WriteFile(confPath, []byte(s), 0644)
}

// configure tor to bind to subnet
func up(subnet string) error {
	debug("configuring", subnet, "as up")
	s, err := read()
	if err != nil {
		return err
	}

	tp := "TransPort 192.168." + subnet + ".1:9040"
	dp := "DNSPort 192.168." + subnet + ".1:5353"
	s = strings.Replace(s, "#"+tp, " "+tp, 1)
	s = strings.Replace(s, "#"+dp, " "+dp, 1)

	write(s)
	return nil
}

// configure tor to not bind to subnet
func down(subnet string) error {
	debug("configuring", subnet, "as down")
	s, err := read()
	if err != nil {
		return err
	}

	tp := "TransPort 192.168." + subnet + ".1:9040"
	dp := "DNSPort 192.168." + subnet + ".1:5353"
	s = strings.Replace(s, " "+tp, "#"+tp, 1)
	s = strings.Replace(s, " "+dp, "#"+dp, 1)

	write(s)
	return nil
}

func main() {
	debug("Device plugged in, running net configurator version ", version)

	// which interface changed?
	subnet := ""
	iface := os.Getenv("IFACE")
	if iface == "eth1" {
		subnet = "43"
	}
	iface = os.Getenv("INTERFACE")
	if iface == "wlan0" {
		subnet = "42"
	}
	if subnet == "" {
		debug("Not our interface.")
		os.Exit(0)
	}

	// was it added or removed
	action := NONE
	mode := os.Getenv("MODE")
	if mode == "start" {
		action = START
	} else if mode == "stop" {
		action = STOP
	}
	mode = os.Getenv("ACTION")
	if mode == "add" {
		action = START
	} else if mode == "remove" {
		action = STOP
	}
	if action == NONE {
		debug("Unknown action.")
		os.Exit(0)
	}

	// lock angainst concurrent instances of this program
	flock, err := os.Create(lockPath)
	if err != nil {
		debug(err)
		os.Exit(1)
	}
	defer flock.Close()
	for {
		err = syscall.Flock(int(flock.Fd()), syscall.LOCK_EX)
		if err == syscall.EINTR {
			continue
		}
		break
	}
	if err != nil {
		debug(err)
		os.Exit(1)
	}
	defer syscall.Flock(int(flock.Fd()), syscall.LOCK_UN)

	// change tor config
	if action == START {
		err = up(subnet)
	} else if action == STOP {
		err = down(subnet)
	}
	if err != nil {
		debug(err)
		os.Exit(1)
	}

	// restart the appropriate services
	// Hostapd is rather finicky, which is why the ip configuration is done
	// manually here.
	stopService("isc-dhcp-server")

	// start hostapd and configure the wireless interface
	if iface == "wlan0" {
		// reset to unconfigured state
		stopService("hostapd")
		ipDown("wlan0")
		ipFlush("wlan0")
		if action == START {
			startService("hostapd")
			time.Sleep(10 * time.Second)
			// configure interface
			err = ipAddr("wlan0", "42")
			if err != nil {
				debug(err)
			}
			err = ipUp("wlan0")
			if err != nil {
				debug(err)
			}
		}
	}

	startService("isc-dhcp-server")
}
