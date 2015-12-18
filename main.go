package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/codegangsta/cli"
	"github.com/ekristen/queryable/resolver"
	dockerapi "github.com/fsouza/go-dockerclient"
)

var Build string
var Version string

func getopt(name, def string) string {
	if env := os.Getenv(name); env != "" {
		return env
	}
	return def
}

func ipAddress() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && !ipnet.IP.IsMulticast() {
			if ipv4 := ipnet.IP.To4(); ipv4 != nil {
				return ipv4.String(), nil
			}
		}
	}

	return "", errors.New("no addresses found")
}

func parseContainerEnv(containerEnv []string, prefix string) map[string]string {
	parsed := make(map[string]string)

	for _, env := range containerEnv {
		if !strings.HasPrefix(env, prefix) {
			continue
		}
		keyVal := strings.SplitN(env, "=", 2)
		if len(keyVal) > 1 {
			parsed[keyVal[0]] = keyVal[1]
		} else {
			parsed[keyVal[0]] = ""
		}
	}

	return parsed
}

func registerContainers(docker *dockerapi.Client, events chan *dockerapi.APIEvents, dns resolver.Resolver, containerDomain string, hostIP net.IP, noDNSResolves bool) error {
	// TODO add an options struct instead of passing all as parameters
	// though passing the events channel from an options struct was triggering
	// data race warnings within AddEventListener, so needs more investigation

	if events == nil {
		events = make(chan *dockerapi.APIEvents)
	}
	if err := docker.AddEventListener(events); err != nil {
		return err
	}

	if !strings.HasPrefix(containerDomain, ".") {
		containerDomain = "." + containerDomain
	}

	getAddress := func(container *dockerapi.Container) (net.IP, error) {
		for {
			if container.NetworkSettings.IPAddress != "" {
				return net.ParseIP(container.NetworkSettings.IPAddress), nil
			}

			if container.HostConfig.NetworkMode == "host" {
				if hostIP == nil {
					return nil, errors.New("IP not available with network mode \"host\"")
				} else {
					return hostIP, nil
				}
			}

			if container.NetworkSettings.Networks[container.HostConfig.NetworkMode].IPAddress != "" {
				return net.ParseIP(container.NetworkSettings.Networks[container.HostConfig.NetworkMode].IPAddress), nil
			}

			if strings.HasPrefix(container.HostConfig.NetworkMode, "container:") {
				otherId := container.HostConfig.NetworkMode[len("container:"):]
				var err error
				container, err = docker.InspectContainer(otherId)
				if err != nil {
					return nil, err
				}
				continue
			}

			return nil, fmt.Errorf("unknown network mode", container.HostConfig.NetworkMode)
		}
	}

	addContainer := func(containerId string) error {
		container, err := docker.InspectContainer(containerId)
		if err != nil {
			return err
		}
		addr, err := getAddress(container)
		if err != nil {
			return err
		}

		err = dns.AddHost(containerId, addr, container.Config.Hostname, container.Name[1:], container.Name[1:]+containerDomain)
		if err != nil {
			return err
		}

		if noDNSResolves == false {
			env := parseContainerEnv(container.Config.Env, "DNS_")
			if dnsDomains, ok := env["DNS_RESOLVES"]; ok {
				if dnsDomains == "" {
					return errors.New("empty DNS_RESOLVES, should contain a comma-separated list with at least one domain")
				}

				port := 53
				if portString := env["DNS_PORT"]; portString != "" {
					port, err = strconv.Atoi(portString)
					if err != nil {
						return errors.New("invalid DNS_PORT \"" + portString + "\", should contain a number")
					}
				}

				domains := strings.Split(dnsDomains, ",")
				err = dns.AddUpstream(containerId, addr, port, domains...)
				if err != nil {
					return err
				}
			}
		}

		if bridge := container.NetworkSettings.Bridge; bridge != "" {
			bridgeAddr := net.ParseIP(container.NetworkSettings.Gateway)
			err = dns.AddHost("bridge:"+bridge, bridgeAddr, bridge)
			if err != nil {
				return err
			}
		}

		return nil
	}

	containers, err := docker.ListContainers(dockerapi.ListContainersOptions{})
	if err != nil {
		return err
	}

	for _, listing := range containers {
		if err := addContainer(listing.ID); err != nil {
			log.Printf("error adding container %s: %s\n", listing.ID[:12], err)
		}
	}

	if err = dns.Listen(); err != nil {
		return err
	}
	defer dns.Close()

	for msg := range events {
		go func(msg *dockerapi.APIEvents) {
			switch msg.Status {
			case "start":
				if err := addContainer(msg.ID); err != nil {
					log.Printf("error adding container %s: %s\n", msg.ID[:12], err)
				}
			case "die":
				dns.RemoveHost(msg.ID)
				dns.RemoveUpstream(msg.ID)
			}
		}(msg)
	}

	return errors.New("docker event loop closed")
}

func run(domain string, port int, recursors []string, hostIPstr string, noDNSResolves bool) error {
	// set up the signal handler first to ensure cleanup is handled if a signal is
	// caught while initializing
	exitReason := make(chan error)
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		sig := <-c
		log.Println("exit requested by signal:", sig)
		exitReason <- nil
	}()

	log.Printf("Version: %s, Build: %s", Version, Build)

	docker, err := dockerapi.NewClient(getopt("DOCKER_HOST", "unix:///tmp/docker.sock"))
	if err != nil {
		return err
	}

	address, err := ipAddress()
	if err != nil {
		return err
	}
	log.Println("Got Local Address:", address)

	var hostIP net.IP
	if hostIPstr != "" {
		hostIP = net.ParseIP(hostIPstr)
		log.Println("Using Address for --net=host:", hostIP)
	}

	dnsResolver, err := resolver.NewResolver()
	if err != nil {
		return err
	}

	dnsResolver.Port = port
	dnsResolver.Recursors = recursors

	defer dnsResolver.Close()

	localDomain := domain
	dnsResolver.AddUpstream(localDomain, nil, 0, localDomain)

	go func() {
		dnsResolver.Wait()
		exitReason <- errors.New("dns resolver exited")
	}()
	go func() {
		exitReason <- registerContainers(docker, nil, dnsResolver, localDomain, hostIP, noDNSResolves)
	}()

	return <-exitReason
}

func main() {
	app := cli.NewApp()
	app.Name = "queryable"
	app.Usage = "docker dns daemon"
	app.Version = "0.1"
	app.Flags = []cli.Flag{
		cli.IntFlag{
			Name:   "port",
			Value:  53,
			Usage:  "port to listen for queries on",
			EnvVar: "PORT",
		},
		cli.StringSliceFlag{
			Name:   "recursor",
			Usage:  "version of the dns daemon",
			EnvVar: "RECURSOR",
		},
		cli.StringFlag{
			Name:   "domain",
			Value:  "docker",
			Usage:  "local top level domain for containers",
			EnvVar: "DOMAIN",
		},
		cli.StringFlag{
			Name:   "host-ip",
			Usage:  "The IP to use for Host Mode",
			EnvVar: "HOST_IP",
		},
	}

	app.Action = func(c *cli.Context) {
		err := run(c.String("domain"), c.Int("port"), c.StringSlice("recursor"), c.String("host-ip"), c.Bool("no-dns-resolves"))
		if err != nil {
			log.Fatal("queryable: ", err)
		}
	}

	app.Run(os.Args)
}
