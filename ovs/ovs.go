// Copyright (C) 2015 Nippon Telegraph and Telephone Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"github.com/osrg/gobgp/api"
	"fmt"
	"net"
	"strings"
	"os/exec"
)

const (
	ArpResponderFlowTemplate = "priority=%d,dl_type=0x0806,nw_dst=%s,actions=move:NXM_OF_ETH_SRC[]->NXM_OF_ETH_DST[],mod_dl_src:%s,load:0x2->NXM_OF_ARP_OP[],move:NXM_NX_ARP_SHA[]->NXM_NX_ARP_THA[],move:NXM_OF_ARP_SPA[]->NXM_OF_ARP_TPA[],load:0x%s->NXM_NX_ARP_SHA[],load:0x%s->NXM_OF_ARP_SPA[],output:in_port"
	RemotePortSelectionFlowTemplate = "priority=%d,dl_dst=%s,actions=mod_vlan_vid:%d,output:%s"
	LocalPortSelectionFlowTemplate  = "priority=%d,dl_dst=%s,dl_vlan=%d,actions=strip_vlan,output:%s"
)

func Ipv4ToBytesStr(ip net.IP) string {
	ip = ip.To4()
	return fmt.Sprintf("%02x%02x%02x%02x", ip[0], ip[1], ip[2], ip[3])
}

func MacAddressToBytesStr(mac net.HardwareAddr) string {
	return fmt.Sprintf("%02x%02x%02x%02x%02x%02x", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

func addOvsArpResponderFlow(n *api.EVPNNlri, nexthop string, myIp string) {
	if nexthop == myIp || nexthop == "0.0.0.0" {
		// Never add an Arp responder flow for a container running on myself
		return
	}

 	ip := net.ParseIP(n.MacIpAdv.IpAddr)
 	mac, _ := net.ParseMAC(n.MacIpAdv.MacAddr)

	fmt.Printf("Add an ArpResponder flow for the container %s on %s\n", ip.String(), nexthop)
	flow := fmt.Sprintf(ArpResponderFlowTemplate, 100, ip, mac, MacAddressToBytesStr(mac), Ipv4ToBytesStr(ip))

	_, err := exec.Command("ovs-ofctl", "add-flow", "docker0-ovs", flow).Output()

	if err != nil {
		fmt.Println(err)
	}
}


func addOvsRemotePortSelectionFlow(n *api.EVPNNlri, nexthop string, myIp string) {
	if nexthop == myIp || nexthop == "0.0.0.0" {
		// Never add a remote port selection flow for a container running on myself
		return
	}

	vni := n.MacIpAdv.Labels[0]
 	ip := net.ParseIP(n.MacIpAdv.IpAddr)
	fmt.Printf("Add a RemotePortSelection flow for the container %s (vlan: %d) on %s\n", ip.String(), vni, nexthop)

	// retrieve the port number to send packets for the new container
	command := fmt.Sprintf("ovs-ofctl show docker0-ovs | grep %s | sed -e \"s/[^0-9]*\\([0-9]*\\)(.*/\\1/\"", nexthop)

	out, err := exec.Command("sh", "-c", command).Output()

	if err != nil {
		fmt.Println("Error: cannot find the appropriate port to send packets.")
		return
	}

	port := strings.Trim(string(out), "\n")

	// add a flow
 	mac, _ := net.ParseMAC(n.MacIpAdv.MacAddr)
	flow := fmt.Sprintf(RemotePortSelectionFlowTemplate, 50, mac, vni, port)

	_, err = exec.Command("ovs-ofctl", "add-flow", "docker0-ovs", flow).Output()

	if err != nil {
		fmt.Println(err)
	}
}

func addOvsLocalPortSelectionFlow(n *api.EVPNNlri, nexthop string, myIp string) {
	if nexthop != myIp && nexthop != "0.0.0.0" {
		// Never add a local port selection flow for a container running on other peers
		return
	}

	vni := n.MacIpAdv.Labels[0]
 	mac, _ := net.ParseMAC(n.MacIpAdv.MacAddr)
	ip := net.ParseIP(n.MacIpAdv.IpAddr)
	fmt.Printf("Add a LocalPortSelection flow for the container %s (vlan: %d)\n", mac.String(), vni)

	containers, networks := GetContainersInfo()
	var portName string

	for _, info := range containers {
		// why does net.HardwareAddr not have Equal method??
		if (ip.Equal(info.Ip) && mac.String() == info.Mac.String() && networks[info.Network].Vni == int(vni)) {
			portName = info.PortName
		}
	}

	command := "ovs-ofctl show docker0-ovs | grep -e \"" + portName + "\" | sed -e \"s/ *\\([0-9]*\\)(.*/\\1/\""
	out, err := exec.Command("sh", "-c", command).Output()

	if err != nil {
		fmt.Printf("Error: cannot find the local port %s\n", portName)
		return
	}

	port := string(out)

	// build a flow as a string
	flow := fmt.Sprintf(LocalPortSelectionFlowTemplate, 50, mac, vni, port)

	// add a flow
	_, err = exec.Command("ovs-ofctl", "add-flow", "docker0-ovs", flow).Output()

	if err != nil {
		fmt.Println(err)
	}
}

// get the IP address of myself (there should be an easier way?)
func getMyIp(iface string) string {
	myIp := ""
	command := fmt.Sprintf("/sbin/ifconfig %s | grep \"inet addr\" | sed \"s/.*inet addr:\\([0-9.]*\\).*/\\1/\"", iface)

	out, err := exec.Command("sh", "-c", command).Output()

	if err != nil {
		fmt.Println(err)
		myIp = "0.0.0.0"
	} else {
		myIp = strings.Trim(string(out), "\n")
	}

	return myIp
}

func addOvsFlows(n *api.EVPNNlri, nexthop string) {
	// TODO: select appropriate iface automatically
	addOvsArpResponderFlow(n, nexthop, getMyIp("eth1"))
	addOvsRemotePortSelectionFlow(n, nexthop, getMyIp("eth1"))
	addOvsLocalPortSelectionFlow(n, nexthop, getMyIp("eth1"))
}
