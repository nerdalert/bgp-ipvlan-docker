bgp-ipvlan-docker
=================

This is a basic functioning Docker network driver for a Hackday #3 event Friday Sept 18th.

If you ask some of us old timers in networking what mechanism has the most scale for distributing network state 9/10 times the answer should be BGP. Docker/containerization will drive endpoint density significantly over the next few years. Am interested to see how far we could scale using %100 Go and some new Linux kernel port type of IPVlan L3 mode (No more proxying ARP, umm yes please). Code is a mess as its 3am the night before the event :p Not responsible for any cursing in the comments. Will probably need to version this with Godep as there are a good number of libs in here and I havent tested from a fresh host yet.

- IPVlan is a lightweight L2 and L3 network implementation that does not require traditional bridges.

- BGP is what makes the Internetz works.

### Pre-Requisites

1. Install the Docker experimental binary from the instructions at: [Docker Experimental](https://github.com/docker/docker/tree/master/experimental). (stop other docker instances)
	- Quick Experimental Install: `wget -qO- https://experimental.docker.com/ | sh`

2. Install the BGP daemon. This is temporary and should be in a container time permitting. Clone the Gobgp repo (OSRG guys are awesomes, hands down some of the best network programmers out there):

```
git clone https://github.com/osrg/gobgp.git
cd gobgp
go get ./...

# Verify the binary built with:
gobgp --help
```

###  L3 Multi-Host BGP Mode

Here are two example hosts configurations that will peer to one another. You can also stick Quagga or any other BGP speaker like any network router or some of the software routers becoming more pervasive. BGP is being used for prefix and next hop. This is an underlay PoC. Just need to add VXLAN ifaces where the BGP attribute originating address enables you to infer whether the endpoint is on the same network segment e.g. overlay/vxlan where the nexthop matches the prefix origin or the nexthop and origin don't match infering an underlying physical network is between the desitnation IP prefix (example. /32 host route at nexthop/origin/endpoint/docker_host is at the IP 192.168.1.100):

```
#----------------------------------#
  Host #1 - 192.168.1.248 (client)
#----------------------------------#
[Global]
  [Global.GlobalConfig]
    As = 65000
    RouterId = "192.168.1.248"

[Neighbors]
  [[Neighbors.NeighborList]]
    [Neighbors.NeighborList.NeighborConfig]
      NeighborAddress = "192.168.1.251"
      PeerAs = 65000

#----------------------------------#
  Host #2 - 192.168.1.251 (client)
#----------------------------------#
[Global]
  [Global.GlobalConfig]
    As = 65000
    RouterId = "192.168.1.251"

[Neighbors]
  [[Neighbors.NeighborList]]
    [Neighbors.NeighborList.NeighborConfig]
      NeighborAddress = "192.168.1.248"
      PeerAs = 65000

```

Start the BGP daemon:

```
cd ~/go/src/github.com/osrg/gobgp
go run gobgpd/main.go --config-file=bgp-conf-248.toml
```

Start the Docker daemon passing the arguments as shown below:

```
    docker daemon --default-network=ipvlan:foo
```

Start the driver that uses docker/libnetwork. Using /24 per host until I figure out how to do /32 host routes which I think is possible soon w/ the new pluggable ipam libnetwork feature, sweet!:

```
$ ./ipvlan-docker-plugin \
        --host-interface eth1 \
        --ipvlan-subnet=10.1.1.0/24 \
        --mode=l3

# Or with Go using:
go run main.go --host-interface eth1 -d --mode l3 --ipvlan-subnet=10.1.1.0/24
```

Lastly start up some containers and check reachability:

```
docker run -i -t --rm ubuntu
```

Example BGP Update from logs when a container comes online or a new IP prefix learned from either hardware peerings or docker host to host peerings:

- In this case, a Docker host just created a container on the network 172.16.1.100/24 which triggered the subnet create and subsequent BGP update that network `172.16.1.100/24` is now found on the docker host `192.168.1.250`. Easy peasy, from there a netlink route is added into the rest of the docker hosts when they receive the update and all of the hosts in the BGP cluster can now reach one another. This explicitly seperates packet forwarding from L4-L7 to allow for seperate evolvable paths that often have different consistency needs and ops responsibilities.
```
INFO[0000] Adding route learned via BGP for a remote endpoint with:
INFO[0000] IP Prefix: [ 172.16.1.100/24 ] - Next Hop: [ 192.168.1.250 ] - Source Interface: [ eth1 ]
```