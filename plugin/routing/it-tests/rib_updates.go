package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"

	log "github.com/Sirupsen/logrus"
)

func main() {
	cleanExistingRoutes("eth1")
}

func InitBGP() {

	err := cleanExistingRoutes(ethIface)
	if err != nil {
		log.Infof("Error cleaning old routes: %s", err)
	}

	//	// Test Add
	err = OriginateBgpRoute("248.1.1.201/32")
	if err != nil {
		log.Infof("Error installing container rooute into BGP: %s", err)
	}

	// Read all existing routes from BGP peers
	ribSlice := readExistingRib()
	if err != nil {
		log.Infof("Error getting existing routes", err)
	}
	log.Infof("Adding Netlink routes the learned BGP routes: \n %s ", ribSlice)
	bgpCache := &RibCache{
		BgpTable: make(map[string]*RibLocal),
	}
	learnedRoutes := bgpCache.handleRouteList2(ribSlice)

	for n, entry := range learnedRoutes {
		fmt.Printf("%d - %+v \n", n+1, entry)

		err = addLinuxRoute(entry.BgpPrefix, entry.NextHop, ethIface)
		if err != nil {
			log.Errorf("Error installing learned bgp route [ %s ]", err)
		}

	}

	// move to a new function "Monitor RIB"
	cmd := exec.Command(bgpCmd, monitor, global, rib, jsonFmt)
	log.Warn("Next running bgp monitor cmd")

	cmdStdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Panicf("Error reading file with tail: %s", err)
	}
	cmdReader := bufio.NewReader(cmdStdout)
	cmd.Start()

	log.Warn("Executed bgp monitor cmd")

	for {
		line, err := cmdReader.ReadString('\n')
		log.Infof("Raw json message %s ", line)
		if err != nil {
			log.Error("Error reading JSON: %s", err)
		}
		var msg RibMonitor
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			log.Error(err)
		}
		// Monitor and process the BGP update
		monitorUpdate, err := bgpCache.handleBgpRibMonitor(msg)
		if err != nil {
			log.Errorf("error processing bgp update [ %s ]", err)
		}
		if monitorUpdate.IsLocal != true {
			if isWithdrawn(line) {
				monitorUpdate.IsWithdraw = true
				log.Infof("BGP update has [ withdrawn ] the IP prefix [ %s ]", monitorUpdate.BgpPrefix.String())

				err = delLinuxRoute(monitorUpdate.BgpPrefix, monitorUpdate.NextHop, ethIface)
				if err != nil {
					log.Errorf("Error installing learned bgp route [ %s ]", err)
				}

			} else {
				monitorUpdate.IsWithdraw = false
				learnedRoutes = append(learnedRoutes, *monitorUpdate)
				err = addLinuxRoute(monitorUpdate.BgpPrefix, monitorUpdate.NextHop, ethIface)
				if err != nil {
					log.Errorf("Error installing learned bgp route [ %s ]", err)
				}

				log.Infof("Updated the local prefix cache from the newly learned BGP update:")
				for n, entry := range learnedRoutes {
					fmt.Printf("%d - %+v \n", n+1, entry)
				}

			}
		}
		log.Infof("Update details:\n%s", monitorUpdate)
	}
}

func deleteRouteCache(value string, list []string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func (cache *RibCache) handleBgpRibMonitor(routeMonitor RibMonitor) (*RibLocal, error) {
	ribLocal := &RibLocal{}

	log.Errorf("BGP update for prefix [ %v ]", routeMonitor.Nlri.Prefix)

	if routeMonitor.Nlri.Prefix != "" {

		bgpPrefix, err := ParseIPNet(routeMonitor.Nlri.Prefix)

		if err != nil {
			log.Errorf("Error parsing the bgp update prefix")
		}
		ribLocal.BgpPrefix = bgpPrefix
	}

	for _, attr := range routeMonitor.Attrs {

		log.Printf("Type: [ %d ] Value: [ %s ]", attr.Type, attr.Value)
		switch attr.Type {
		case BGP_ATTR_TYPE_ORIGIN:
			// 0 = iBGP; 1 = eBGP
			if attr.Value != nil {
				log.Infof("Type Code: [ %d ] Origin: %g", BGP_ATTR_TYPE_ORIGIN, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_AS_PATH:
			if attr.Value != nil {
				log.Infof("Type Code: [ %d ] AS_Path: %s", BGP_ATTR_TYPE_AS_PATH, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_NEXT_HOP:
			if attr.Nexthop != "" {
				log.Infof("Type Code: [ %d ] Nexthop: %s", BGP_ATTR_TYPE_NEXT_HOP, attr.Nexthop)
				ribLocal.NextHop = net.ParseIP(attr.Nexthop)
				if ribLocal.NextHop.String() == localNexthop {
					ribLocal.IsLocal = true
				}
			}
		case BGP_ATTR_TYPE_MULTI_EXIT_DISC:
			if attr.Value != nil {
				log.Infof("Type Code: [ %d ] MED: %g", BGP_ATTR_TYPE_MULTI_EXIT_DISC, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_LOCAL_PREF:
			if attr.Value != nil {
				log.Infof("Type Code: [ %d ] Local Pref: %g", BGP_ATTR_TYPE_LOCAL_PREF, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_ORIGINATOR_ID:
			if attr.Value != nil {
				log.Infof("Type Code: [ %d ] Originator IP: %s", BGP_ATTR_TYPE_ORIGINATOR_ID, attr.Nexthop)
				ribLocal.OriginatorIP = net.ParseIP(attr.Value.(string))
				log.Infof("Type Code: [ %d ] Originator IP: %s", BGP_ATTR_TYPE_ORIGINATOR_ID, ribLocal.OriginatorIP)
			}
		case BGP_ATTR_TYPE_CLUSTER_LIST:
			if len(attr.ClusterList) > 0 {
				log.Infof("Type Code: [ %d ] Cluster List: %s", BGP_ATTR_TYPE_CLUSTER_LIST, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_MP_REACH_NLRI:
			if attr.Value != nil {
				log.Infof("Type Code: [ %d ] MP Reachable: %v", BGP_ATTR_TYPE_MP_REACH_NLRI, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_MP_UNREACH_NLRI:
			if attr.Value != nil {
				log.Infof("Type Code: [ %d ]  MP Unreachable: %v", BGP_ATTR_TYPE_MP_UNREACH_NLRI, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_EXTENDED_COMMUNITIES:
			if attr.Value != nil {
				log.Infof("Type Code: [ %d ] Extended Communities: %v", BGP_ATTR_TYPE_EXTENDED_COMMUNITIES, attr.Nexthop)
			}
		default:
			log.Errorf("Unknown BGP attribute code [ %d ]")
		}
	}
	return ribLocal, nil

}

func (cache *RibCache) handleRouteList2(jsonMsg string) []RibLocal {
	var ribIn []RibIn
	localRib := &RibLocal{}
	localRibList := []RibLocal{}
	if err1 := json.Unmarshal([]byte(jsonMsg), &ribIn); err1 != nil {
		fmt.Println("Err Json: ", err1.Error())
	} else {
		for _, routeIn := range ribIn {
			for _, paths := range routeIn.Paths {
				if routeIn.Prefix != "" {
					bgpPrefix, err := ParseIPNet(routeIn.Prefix)
					if err != nil {
						log.Errorf("Error parsing the bgp update prefix")
					}
					localRib.BgpPrefix = bgpPrefix
				}
				for _, attr := range paths.Attrs {
					log.Printf("Type: [ %d ] Value: [ %s ]", attr.Type, attr.Value)
					switch attr.Type {
					case BGP_ATTR_TYPE_ORIGIN:
						// 0 = iBGP; 1 = eBGP
						if attr.Value != nil {
							log.Infof("Type Code: [ %d ] Origin: %g", BGP_ATTR_TYPE_ORIGIN, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_AS_PATH:
						if attr.Value != nil {
							log.Infof("Type Code: [ %d ] AS_Path: %s", BGP_ATTR_TYPE_AS_PATH, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_NEXT_HOP:
						if attr.Nexthop != "" {
							log.Infof("Type Code: [ %d ] Nexthop: %s", BGP_ATTR_TYPE_NEXT_HOP, attr.Nexthop)
							localRib.NextHop = net.ParseIP(attr.Nexthop)
							if localRib.NextHop.String() == localNexthop {
								localRib.IsLocal = true
							}
						}
					case BGP_ATTR_TYPE_MULTI_EXIT_DISC:
						if attr.Value != nil {
							log.Infof("Type Code: [ %d ] MED: %g", BGP_ATTR_TYPE_MULTI_EXIT_DISC, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_LOCAL_PREF:
						if attr.Value != nil {
							log.Infof("Type Code: [ %d ] Local Pref: %g", BGP_ATTR_TYPE_LOCAL_PREF, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_ORIGINATOR_ID:
						if attr.Value != nil {
							log.Infof("Type Code: [ %d ] Originator IP: %s", BGP_ATTR_TYPE_ORIGINATOR_ID, attr.Nexthop)
							localRib.OriginatorIP = net.ParseIP(attr.Value.(string))
							log.Infof("Type Code: [ %d ] Originator IP: %s", BGP_ATTR_TYPE_ORIGINATOR_ID, localRib.OriginatorIP)
						}
					case BGP_ATTR_TYPE_CLUSTER_LIST:
						if len(attr.ClusterList) > 0 {
							log.Infof("Type Code: [ %d ] Cluster List: %s", BGP_ATTR_TYPE_CLUSTER_LIST, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_MP_REACH_NLRI:
						if attr.Value != nil {
							log.Infof("Type Code: [ %d ] MP Reachable: %v", BGP_ATTR_TYPE_MP_REACH_NLRI, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_MP_UNREACH_NLRI:
						if attr.Value != nil {
							log.Infof("Type Code: [ %d ]  MP Unreachable: %v", BGP_ATTR_TYPE_MP_UNREACH_NLRI, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_EXTENDED_COMMUNITIES:
						if attr.Value != nil {
							log.Infof("Type Code: [ %d ] Extended Communities: %v", BGP_ATTR_TYPE_EXTENDED_COMMUNITIES, attr.Nexthop)
						}
					default:
						log.Errorf("Unknown BGP attribute code")
					}
				}
			}

			localRibList = append(localRibList, *localRib)
		}

	}
	//	pretty := JsonPrettyPrint(bgpCache.BgpTable )
	//
	//	log.Printf(" [ %s ]",pretty)
	//	return localRib
	log.Infof("Debugging enabled, listing learned routes: %v", localRibList)
	return localRibList
}

// return string representation of pluginConfig for debugging
func (d *RibLocal) stringer() string {
	str := fmt.Sprintf("Prefix:[ %s ], ", d.BgpPrefix.String())
	str = str + fmt.Sprintf("OriginatingIP:[ %s ], ", d.OriginatorIP.String())
	str = str + fmt.Sprintf("Nexthop:[ %s ], ", d.NextHop.String())
	str = str + fmt.Sprintf("IsWithdrawn:[ %t ], ", d.IsWithdraw)
	str = str + fmt.Sprintf("IsHostRoute:[ %t ], \n", d.IsHostRoute)
	return str
}

func JsonPrettyPrint(input string) string {
	var buf bytes.Buffer
	json.Indent(&buf, []byte(input), "", " ")
	return buf.String()
}

func gobgp(cmd string, arg ...string) (bytes.Buffer, bytes.Buffer, error) {
	var stdout, stderr bytes.Buffer
	command := exec.Command(cmd, arg...)
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout, stderr, err
}

// Temporary until addOvsVethPort works
func readExistingRib() string {
	cmd, stderr, err := gobgp(bgpCmd, global, rib, jsonFmt)
	if err != nil {
		log.Errorf("%s", stderr)
		return ""
	}

	return cmd.String()
}

// Temporary until addOvsVethPort works
func monitorBgpRIB(bridge string, port string) error {
	_, stderr, err := gobgp(bgpCmd, monitor, global, rib, jsonFmt)
	if err != nil {
		errmsg := stderr.String()
		return errors.New(errmsg)
	}
	return nil
}

func ParseIPNet(s string) (*net.IPNet, error) {
	ip, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		return nil, err
	}
	return &net.IPNet{IP: ip, Mask: ipNet.Mask}, nil
}

func isWithdrawn(str string) bool {
	isWithdraw, _ := regexp.Compile(`\"isWithdraw":true`)
	return isWithdraw.MatchString(str)
}
