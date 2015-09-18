package routing

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

func InitBGPMonitoring(masterIface string) {
	ethIface = masterIface
	err := cleanExistingRoutes(ethIface)
	if err != nil {
		log.Infof("Error cleaning old routes: %s", err)
	}
	// Read all existing routes from BGP peers
	ribSlice := readExistingRib()
	if err != nil {
		log.Infof("Error getting existing routes", err)
	}
	log.Infof("Adding Netlink routes the learned BGP routes: %s ", ribSlice)
	bgpCache := &RibCache{
		BgpTable: make(map[string]*RibLocal),
	}
	learnedRoutes := bgpCache.handleRouteList2(ribSlice)
	log.Infof("Processing [ %d ] routes learned via BGP", len(learnedRoutes))
	for _, entry := range learnedRoutes {
		// todo: what to do if the learned bgp route
		// overlaps with an existing netlink. This check
		// simply throws an error msg but still adds route
		verifyRoute(entry.BgpPrefix)
		err = addNetlinkRoute(entry.BgpPrefix, entry.NextHop, ethIface)
		if err != nil {
			log.Errorf("Add netlink route results [ %s ]", err)
		}
	}
	cmd := exec.Command(bgpCmd, monitor, global, rib, jsonFmt)
	cmdStdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Panicf("Error reading file with tail: %s", err)
	}
	cmdReader := bufio.NewReader(cmdStdout)
	cmd.Start()
	log.Info("Initialization complete, now monitoring BGP for new routes..")
	for {
		route, err := cmdReader.ReadString('\n')
		log.Infof("Raw json message %s ", route)
		if err != nil {
			log.Error("Error reading JSON: %s", err)
		}
		var msg RibMonitor
		if err := json.Unmarshal([]byte(route), &msg); err != nil {
			log.Error(err)
		}
		// Monitor and process the BGP update
		monitorUpdate, err := bgpCache.handleBgpRibMonitor(msg)
		if err != nil {
			log.Errorf("error processing bgp update [ %s ]", err)
		}
		if monitorUpdate.IsLocal != true {
			if isWithdrawn(route) {
				monitorUpdate.IsWithdraw = true
				log.Infof("BGP update has [ withdrawn ] the IP prefix [ %s ]", monitorUpdate.BgpPrefix.String())
				// If the bgp update contained a withdraw, remove the local netlink route for the remote endpoint
				err = delNetlinkRoute(monitorUpdate.BgpPrefix, monitorUpdate.NextHop, ethIface)
				if err != nil {
					log.Errorf("Error removing learned bgp route [ %s ]", err)
				}
			} else {
				monitorUpdate.IsWithdraw = false
				learnedRoutes = append(learnedRoutes, *monitorUpdate)
				log.Debugf("Learned routes: %v ", monitorUpdate)

				err = addNetlinkRoute(monitorUpdate.BgpPrefix, monitorUpdate.NextHop, ethIface)
				if err != nil {
					log.Debugf("Add route results [ %s ]", err)
				}

				log.Infof("Updated the local prefix cache from the newly learned BGP update:")
				for n, entry := range learnedRoutes {
					log.Debugf("%d - %+v", n+1, entry)
				}
			}
		}
		log.Debugf("Verbose update details: %s", monitorUpdate)
	}
}

func (cache *RibCache) handleBgpRibMonitor(routeMonitor RibMonitor) (*RibLocal, error) {
	ribLocal := &RibLocal{}
	log.Debugf("BGP update for prefix: [ %v ] ", routeMonitor.Nlri.Prefix)
	if routeMonitor.Nlri.Prefix != "" {
		bgpPrefix, err := ParseIPNet(routeMonitor.Nlri.Prefix)
		if err != nil {
			log.Errorf("Error parsing the bgp update prefix")
		}
		ribLocal.BgpPrefix = bgpPrefix
	}
	for _, attr := range routeMonitor.Attrs {
		log.Debugf("Type: [ %d ] Value: [ %s ]", attr.Type, attr.Value)
		switch attr.Type {
		case BGP_ATTR_TYPE_ORIGIN:
			// 0 = iBGP; 1 = eBGP
			if attr.Value != nil {
				log.Debugf("Type Code: [ %d ] Origin: %g", BGP_ATTR_TYPE_ORIGIN, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_AS_PATH:
			if attr.Value != nil {
				log.Debugf("Type Code: [ %d ] AS_Path: %s", BGP_ATTR_TYPE_AS_PATH, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_NEXT_HOP:
			if attr.Nexthop != "" {
				log.Debugf("Type Code: [ %d ] Nexthop: %s", BGP_ATTR_TYPE_NEXT_HOP, attr.Nexthop)
				ribLocal.NextHop = net.ParseIP(attr.Nexthop)
				if ribLocal.NextHop.String() == localNexthop {
					ribLocal.IsLocal = true
				}
			}
		case BGP_ATTR_TYPE_MULTI_EXIT_DISC:
			if attr.Value != nil {
				log.Debugf("Type Code: [ %d ] MED: %g", BGP_ATTR_TYPE_MULTI_EXIT_DISC, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_LOCAL_PREF:
			if attr.Value != nil {
				log.Debugf("Type Code: [ %d ] Local Pref: %g", BGP_ATTR_TYPE_LOCAL_PREF, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_ORIGINATOR_ID:
			if attr.Value != nil {
				log.Debugf("Type Code: [ %d ] Originator IP: %s", BGP_ATTR_TYPE_ORIGINATOR_ID, attr.Nexthop)
				ribLocal.OriginatorIP = net.ParseIP(attr.Value.(string))
				log.Debugf("Type Code: [ %d ] Originator IP: %s", BGP_ATTR_TYPE_ORIGINATOR_ID, ribLocal.OriginatorIP)
			}
		case BGP_ATTR_TYPE_CLUSTER_LIST:
			if len(attr.ClusterList) > 0 {
				log.Debugf("Type Code: [ %d ] Cluster List: %s", BGP_ATTR_TYPE_CLUSTER_LIST, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_MP_REACH_NLRI:
			if attr.Value != nil {
				log.Debugf("Type Code: [ %d ] MP Reachable: %v", BGP_ATTR_TYPE_MP_REACH_NLRI, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_MP_UNREACH_NLRI:
			if attr.Value != nil {
				log.Debugf("Type Code: [ %d ]  MP Unreachable: %v", BGP_ATTR_TYPE_MP_UNREACH_NLRI, attr.Nexthop)
			}
		case BGP_ATTR_TYPE_EXTENDED_COMMUNITIES:
			if attr.Value != nil {
				log.Debugf("Type Code: [ %d ] Extended Communities: %v", BGP_ATTR_TYPE_EXTENDED_COMMUNITIES, attr.Nexthop)
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
	if unmarshallErr := json.Unmarshal([]byte(jsonMsg), &ribIn); unmarshallErr != nil {
		fmt.Println("Unmarshaling json error: ", unmarshallErr.Error())
	} else {
		for _, routeIn := range ribIn {
			for _, paths := range routeIn.Paths {
				if routeIn.Prefix != "" {
					bgpPrefix, err := ParseIPNet(routeIn.Prefix)
					if err != nil {
						log.Errorf("Error parsing the bgp update prefix")
					}
					localRib.BgpPrefix = bgpPrefix
					log.Debugf("Details for learned BGP Prefix [ %s ]", bgpPrefix.String())
				}
				for _, attr := range paths.Attrs {
					//					log.Printf("Type: [ %d ] Value: [ %s ]", attr.Type, attr.Value)

					switch attr.Type {
					case BGP_ATTR_TYPE_ORIGIN:
						// 0 = iBGP; 1 = eBGP
						//						if attr.Value != nil {
						log.Debugf("Type Code: [ %d ] Origin: %g", BGP_ATTR_TYPE_ORIGIN, attr.Value)
						//						}
					case BGP_ATTR_TYPE_AS_PATH:
						if attr.AsPaths != "" {
							log.Debugf("Type Code: [ %d ] AS_Path: %s", BGP_ATTR_TYPE_AS_PATH, attr.AsPaths)
						}
					case BGP_ATTR_TYPE_NEXT_HOP:
						if attr.Nexthop != "" {
							log.Debugf("Type Code: [ %d ] Nexthop: %s", BGP_ATTR_TYPE_NEXT_HOP, attr.Nexthop)
							localRib.NextHop = net.ParseIP(attr.Nexthop)
							if localRib.NextHop.String() == localNexthop {
								localRib.IsLocal = true
							}
						}
					case BGP_ATTR_TYPE_MULTI_EXIT_DISC:
						if attr.Metric != 0 {
							log.Debugf("Type Code: [ %d ] MED: %g", BGP_ATTR_TYPE_MULTI_EXIT_DISC, attr.Metric)
						}
					case BGP_ATTR_TYPE_LOCAL_PREF:
						if attr.Value != nil {
							log.Debugf("Type Code: [ %d ] Local Pref: %g", BGP_ATTR_TYPE_LOCAL_PREF, attr.Value)
						}
					case BGP_ATTR_TYPE_ORIGINATOR_ID:
						if attr.Value != "" {
							localRib.OriginatorIP = net.ParseIP(attr.Value.(string))
							log.Debugf("Type Code: [ %d ] Originator IP: %s", BGP_ATTR_TYPE_ORIGINATOR_ID, localRib.OriginatorIP.String())
						}
					case BGP_ATTR_TYPE_CLUSTER_LIST:
						if len(attr.ClusterList) > 0 {
							log.Debugf("Type Code: [ %d ] Cluster List: %s", BGP_ATTR_TYPE_CLUSTER_LIST, attr.ClusterList)
						}
					case BGP_ATTR_TYPE_MP_REACH_NLRI:
						if attr.Value != nil {
							log.Debugf("Type Code: [ %d ] MP Reachable: %v", BGP_ATTR_TYPE_MP_REACH_NLRI, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_MP_UNREACH_NLRI:
						if attr.Value != nil {
							log.Debugf("Type Code: [ %d ]  MP Unreachable: %v", BGP_ATTR_TYPE_MP_UNREACH_NLRI, attr.Nexthop)
						}
					case BGP_ATTR_TYPE_EXTENDED_COMMUNITIES:
						if attr.Value != nil {
							log.Debugf("Type Code: [ %d ] Extended Communities: %v", BGP_ATTR_TYPE_EXTENDED_COMMUNITIES, attr.Nexthop)
						}
					default:
						log.Errorf("Unknown BGP attribute code")
					}
				}
			}
			localRibList = append(localRibList, *localRib)
		}
	}
	log.Debugf("Verbose json for the [ %d ] routes learned via BGP: %+v", len(localRibList), localRibList)
	return localRibList
}

// return string representation of pluginConfig for debugging
func (d *RibLocal) stringer() string {
	str := fmt.Sprintf("Prefix:[ %s ], ", d.BgpPrefix.String())
	str = str + fmt.Sprintf("OriginatingIP:[ %s ], ", d.OriginatorIP.String())
	str = str + fmt.Sprintf("Nexthop:[ %s ], ", d.NextHop.String())
	str = str + fmt.Sprintf("IsWithdrawn:[ %t ], ", d.IsWithdraw)
	str = str + fmt.Sprintf("IsHostRoute:[ %t ]", d.IsHostRoute)
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
