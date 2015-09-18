package routing

import (
	"errors"
	"fmt"
	"net"

	log "github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

// AddHostRoute adds a route to a device using netlink into the Linux default namespace.
func addNetlinkRoute(neighborNetwork *net.IPNet, nextHop net.IP, netIface string) error {
	iface, err := netlink.LinkByName(netIface)
	if err != nil {
		return err
	}
	log.Infof("Adding route learned via BGP for a remote endpoint with:")
	log.Infof("IP Prefix: [ %s ] - Next Hop: [ %s ] - Source Interface: [ %s ]", neighborNetwork, nextHop, iface.Attrs().Name)
	return netlink.RouteAdd(&netlink.Route{
		Scope:     netlink.SCOPE_UNIVERSE,
		LinkIndex: iface.Attrs().Index,
		Dst:       neighborNetwork,
		Gw:        nextHop,
	})
}

// Advertise the local namespace IP prefixes to the bgp neighbors
func OriginateBgpRoute(localPrefix *net.IPNet) error {
	log.Infof("Adding this hosts container network [ %s ] into the BGP domain", localPrefix)
	_, stderr, err := gobgp(bgpCmd, global, rib, bgpAdd, localPrefix.String(), addrFamily, ipv4)
	if err != nil {
		return errors.New(stderr.String())
	}
	return nil
}

// AddHostRoute adds a route to a device using netlink into the Linux default namespace.
func delNetlinkRoute(neighborNetwork *net.IPNet, nextHop net.IP, netIface string) error {
	iface, err := netlink.LinkByName(netIface)
	if err != nil {
		return err
	}
	log.Infof("Deleting netlink route learned via BGP for the remote endpoint:")
	log.Infof("IP Prefix: [ %s ] - Next Hop: [ %s ] - Source Interface: [ %s ]", neighborNetwork, nextHop, iface.Attrs().Name)
	return netlink.RouteDel(&netlink.Route{
		Scope:     netlink.SCOPE_UNIVERSE,
		LinkIndex: iface.Attrs().Index,
		Dst:       neighborNetwork,
		Gw:        nextHop,
	})
}

// Add a route to the global namespace using the default gateway to determine the iface
func checkAddRoute(dest *net.IPNet, nh net.IP) error {
	gwRoutes, err := netlink.RouteGet(nh)
	if err != nil {
		return fmt.Errorf("route for the next hop %s could not be found: %v", nh, err)
	}
	return netlink.RouteAdd(&netlink.Route{
		Scope:     netlink.SCOPE_UNIVERSE,
		LinkIndex: gwRoutes[0].LinkIndex,
		Gw:        gwRoutes[0].Gw,
		Dst:       dest,
	})
}

// AddHostRoute adds a host-scoped route to a device.
func addRoute(neighborNetwork *net.IPNet, nextHop net.IP, iface netlink.Link) error {
	log.Debugf("Adding route in the default namespace for IPVlan L3 mode with the following:")
	log.Debugf("IP Prefix: [ %s ] - Next Hop: [ %s ] - Source Interface: [ %s ]",
		neighborNetwork, nextHop, iface.Attrs().Name)

	return netlink.RouteAdd(&netlink.Route{
		Scope:     netlink.SCOPE_UNIVERSE,
		LinkIndex: iface.Attrs().Index,
		Dst:       neighborNetwork,
		Gw:        nextHop,
	})
}
