package main

import (
	"net"
)

var ethIface = "eth1"

const (
	bgpCmd       = "gobgp"
	bgpAdd       = "add"
	bgpDel       = "del"
	rib          = "rib"
	jsonFmt      = "-j"
	addrFamily   = "-a"
	monitor      = "monitor"
	global       = "global"
	localNexthop = "0.0.0.0"
	ipv4         = "ipv4"
	ipv6         = "ipv6"
)

const (
	_ = iota
	BGP_ATTR_TYPE_ORIGIN
	BGP_ATTR_TYPE_AS_PATH
	BGP_ATTR_TYPE_NEXT_HOP
	BGP_ATTR_TYPE_MULTI_EXIT_DISC
	BGP_ATTR_TYPE_LOCAL_PREF
	BGP_ATTR_TYPE_ATOMIC_AGGREGATE
	BGP_ATTR_TYPE_AGGREGATOR
	BGP_ATTR_TYPE_COMMUNITIES
	BGP_ATTR_TYPE_ORIGINATOR_ID
	BGP_ATTR_TYPE_CLUSTER_LIST
	_
	_
	_
	BGP_ATTR_TYPE_MP_REACH_NLRI // = 14
	BGP_ATTR_TYPE_MP_UNREACH_NLRI
	BGP_ATTR_TYPE_EXTENDED_COMMUNITIES
	BGP_ATTR_TYPE_AS4_PATH
	BGP_ATTR_TYPE_AS4_AGGREGATOR
	_
	_
	_
	BGP_ATTR_TYPE_PMSI_TUNNEL // = 22
	BGP_ATTR_TYPE_TUNNEL_ENCAP
)

type RibIn struct {
	Prefix string
	Paths  []struct {
		Attrs []struct {
			Type        int         `json:"type"`
			Value       interface{} `json:"value,omitempty"`
			Nexthop     string      `json:"nexthop,omitempty"`
			AsPaths     string      `json:"as_paths,omitempty"`
			Metric      int         `json:"metric,omitempty"`
			ClusterList []string
		} `json:"attrs"`
		Nlri *struct {
			Prefix string `json:"prefix,omitempty"`
		} `json:"nlri,omitempty"`
		Age        int  `json:"age,omitempty"`
		Best       bool `json:"best,omitempty"`
		IsWithdraw bool `json:"isWithdraw,omitempty"`
		Validation int  `json:"Validation,omitempty"`
	}
}

type RibMonitor struct {
	Attrs []struct {
		Type        int         `json:"type"`
		Value       interface{} `json:"value,omitempty"`
		Nexthop     string      `json:"nexthop,omitempty"`
		AsPaths     string      `json:"as_paths,omitempty"`
		Metric      int         `json:"metric,omitempty"`
		ClusterList []string
	} `json:"attrs"`
	Nlri *struct {
		Prefix string `json:"prefix,omitempty"`
	} `json:"nlri,omitempty"`
	Age        int  `json:"age,omitempty"`
	Best       bool `json:"best,omitempty"`
	IsWithdraw bool `json:"isWithdraw,omitempty"`
	Validation int  `json:"Validation,omitempty"`
}

type RibCache struct {
	BgpTable map[string]*RibLocal
}

// Unmarshalled BGP update binding for simplicity
type RibLocal struct {
	BgpPrefix    *net.IPNet
	OriginatorIP net.IP
	NextHop      net.IP
	Age          int
	Best         bool
	IsWithdraw   bool
	IsHostRoute  bool
	IsLocal      bool
	AsPath       string
}

type RibTest struct {
	BgpTable map[string]*RibLocal
}
