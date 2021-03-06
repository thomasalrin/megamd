/*
** Copyright [2013-2015] [Megam Systems]
**
** Licensed under the Apache License, Version 2.0 (the "License");
** you may not use this file except in compliance with the License.
** You may obtain a copy of the License at
**
** http://www.apache.org/licenses/LICENSE-2.0
**
** Unless required by applicable law or agreed to in writing, software
** distributed under the License is distributed on an "AS IS" BASIS,
** WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
** See the License for the specific language governing permissions and
** limitations under the License.
 */
package docker

import (
	"encoding/json"
	"fmt"
	"strings"
	"net"
	"net/http"
	"io/ioutil"
	"bytes"
	"math"
	log "code.google.com/p/log4go"
	"github.com/megamsys/libgo/db"
	"github.com/megamsys/megamd/global"
	"github.com/megamsys/seru/cmd"
	"github.com/megamsys/seru/cmd/seru"
	"github.com/tsuru/config"
	"github.com/fsouza/go-dockerclient"
	"time"
)

func IPRequest(subnet net.IPNet) (net.IP, uint, error) {
	bits := bitCount(subnet)
	bc := int(bits / 8)
	partial := int(math.Mod(bits, float64(8)))
	if partial != 0 {
		bc += 1
	}
	index := global.IPIndex{}
	res, err := index.Get(global.IPINDEXKEY)
	if err != nil {
		log.Error("Error: Riak didn't cooperate:\n%s.", err)
		return nil, 0, err
	}
	
	return getIP(subnet, res.Index+1), res.Index+1, nil
}

// Given Subnet of interest and free bit position, this method returns the corresponding ip address
// This method is functional and tested. Refer to ipam_test.go But can be improved

func getIP(subnet net.IPNet, pos uint) net.IP {
	retAddr := make([]byte, len(subnet.IP))
	copy(retAddr, subnet.IP)

	mask, _ := subnet.Mask.Size()
	var tb, byteCount, bitCount int
	if subnet.IP.To4() != nil {
		tb = 4
		byteCount = (32 - mask) / 8
		bitCount = (32 - mask) % 8
	} else {
		tb = 16
		byteCount = (128 - mask) / 8
		bitCount = (128 - mask) % 8
	}

	for i := 0; i <= byteCount; i++ {
		maskLen := 0xFF
		if i == byteCount {
			if bitCount != 0 {
				maskLen = int(math.Pow(2, float64(bitCount))) - 1
			} else {
				maskLen = 0
			}
		}
		masked := pos & uint((0xFF&maskLen)<<uint(8*i))
		retAddr[tb-i-1] |= byte(masked >> uint(8*i))
	}
	return net.IP(retAddr)
}

func bitCount(addr net.IPNet) float64 {
	mask, _ := addr.Mask.Size()
	if addr.IP.To4() != nil {
		return math.Pow(2, float64(32-mask))
	} else {
		return math.Pow(2, float64(128-mask))
	}
}

func testAndSetBit(a []byte) uint {
	var i uint
	for i = uint(0); i < uint(len(a)*8); i++ {
		if !testBit(a, i) {
			setBit(a, i)
			return i + 1
		}
	}
	return i
}

func testBit(a []byte, k uint) bool {
	return ((a[k/8] & (1 << (k % 8))) != 0)
}

func setBit(a []byte, k uint) {
	a[k/8] |= 1 << (k % 8)
}

func setContainerNAL(containerID string, containerName string, endpoint string) (string, error) {   
	
	/*
	* generate the ip 
	*/
	subnetip, _ := config.GetString("docker:subnet")
	_, subnet, _ := net.ParseCIDR(subnetip)
	ip, pos, iperr := IPRequest(*subnet)
	if iperr != nil {
		log.Error("Ip generation was failed : %s", iperr)
		return "", iperr
	}
	client, _ := docker.NewClient(endpoint)
	ch := make(chan bool)
	/*
	* configure ip to container
	*/
	go recv(containerID, containerName, ip.String(), client, ch)
		
	uerr := updateIndex(ip.String(), pos)
	if uerr != nil {
		log.Error("Ip index update was failed : %s", uerr)
	}
	return ip.String(), nil
}

/*
*
* UpdateComponent updates the ipaddress that is bound to the container
* It talks to riakdb and updates the respective component(s)
 */
func updateContainerJSON(assembly *global.AssemblyWithComponents, ipaddress string, containerID string, endpoint string) {

    
	var port string

	//for k, _ := range container_network.Ports {
		//porti := strings.Split(string(k), "/")
		//port = porti[0]
	//}
	port = ""
	fmt.Println(port)	
    
	log.Debug("Update process for component with ip and container id")
	mySlice := make([]*global.KeyValuePair, 3)
	mySlice[0] = &global.KeyValuePair{Key: "ip", Value: ipaddress}
	mySlice[1] = &global.KeyValuePair{Key: "id", Value: containerID}
	mySlice[2] = &global.KeyValuePair{Key: "port", Value: port}
	mySlice[2] = &global.KeyValuePair{Key: "endpoint", Value: endpoint}

	update := global.Component{
		Id:                assembly.Components[0].Id,
		Name:              assembly.Components[0].Name,
		ToscaType:         assembly.Components[0].ToscaType,
		Inputs:            assembly.Components[0].Inputs,
		Outputs:           mySlice,
		Artifacts:         assembly.Components[0].Artifacts,
		RelatedComponents: assembly.Components[0].RelatedComponents,
		Operations:        assembly.Components[0].Operations,
		Status:            assembly.Components[0].Status,
		CreatedAt:         assembly.Components[0].CreatedAt,
	}

	conn, connerr := db.Conn("components")
	if connerr != nil {
		log.Error("Failed to riak connection : %s", connerr)
	}

	err := conn.StoreStruct(assembly.Components[0].Id, &update)
	if err != nil {
		log.Error("Failed to store the update component data : %s", err)
	}
	log.Info("Container component update was successfully.")
}

func GetMemory() int64 {
	memory, _ := config.GetInt("docker:memory")
	return int64(memory)
}

func GetSwap() int64 {
	swap, _ := config.GetInt("docker:swap")
	return int64(swap)

}

func GetCpuPeriod() int64 {
	cpuPeriod, _ := config.GetInt("docker:cpuperiod")
	return int64(cpuPeriod)

}

func GetCpuQuota() int64 {
	cpuQuota, _ := config.GetInt("docker:cpuquota")
	return int64(cpuQuota)

}

func recv(containerID string, containerName string, ip string, client *docker.Client, ch chan bool) {
    log.Info("Receiver waited for container up")
	time.Sleep(18000 * time.Millisecond)
	
    /*
	 * Inspect API is called to fetch the data about the launched container
	 *
	 */
	inscontainer, _ := client.InspectContainer(containerID)
	contain := &docker.Container{}
	mapC, _ := json.Marshal(inscontainer)
	json.Unmarshal([]byte(string(mapC)), contain)
	
	container_state := &docker.State{}
	mapN, _ := json.Marshal(contain.State)
	json.Unmarshal([]byte(string(mapN)), container_state)
	
    if container_state.Running == true {
    	postnetwork(containerID, ip)
    	postlogs(containerID, containerName)
        ch <- true        
        return
    }
    
    go recv(containerID, containerName, ip, client, ch)
}

func postnetwork(containerid string, ip string) {		
	gulpUrl, _ := config.GetString("docker:gulp_url")
	url := gulpUrl + "docker/networks"
    log.Info("URL:> %s", url)

	bridge, _ := config.GetString("docker:bridge")
	gateway, _ := config.GetString("docker:gateway")
	
    data := &global.DockerNetworksInfo{Bridge: bridge, ContainerId: containerid, IpAddr: ip, Gateway: gateway} 
	res2B, _ := json.Marshal(data)
    req, err := http.NewRequest("POST", url, bytes.NewBuffer(res2B))
    req.Header.Set("X-Custom-Header", "myvalue")
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Error("gulpd client was failed : %s", err)
    }
    defer resp.Body.Close()

    log.Info("response Status : %s", resp.Status)
    log.Info("response Headers : %s", resp.Header)
    body, _ := ioutil.ReadAll(resp.Body)
    log.Info("response Body : %s", string(body))   
}

func postlogs(containerid string, containername string) error {
	gulpUrl, _ := config.GetString("docker:gulp_url")
	url := gulpUrl + "docker/logs"
	log.Info("URL:> %s", url)

	data := &global.DockerLogsInfo{ContainerId: containerid, ContainerName: containername}
	res2B, _ := json.Marshal(data)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(res2B))
	req.Header.Set("X-Custom-Header", "myvalue")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Error("gulpd client was failed : %s", err)
	}
	defer resp.Body.Close()

	log.Info("response Status : %s", resp.Status)
    log.Info("response Headers : %s", resp.Header)
    body, _ := ioutil.ReadAll(resp.Body)
    log.Info("response Body : %s", string(body)) 
    return nil
}


func updateIndex(ip string, pos uint) error{

	index := global.IPIndex{}
	res, err := index.Get(global.IPINDEXKEY)
	if err != nil {
		log.Error("Error: Riak didn't cooperate:\n%s.", err)
		return err
	}

	update := global.IPIndex{
		Ip:			ip, 			
		Subnet: 	res.Subnet,
		Index:		pos,
	}

	conn, connerr := db.Conn("ipindex")
	if connerr != nil {
		log.Error("Failed to riak connection : %s", connerr)
		return connerr
	}

	serr := conn.StoreStruct(global.IPINDEXKEY, &update)
	if serr != nil {
		log.Error("Failed to store the update index value : %s", serr)
		return serr
	}
	log.Info("Docker network index update was successfully.")
	return nil
}

/*
* Register a hostname on AWS Route53 using megam seru -
*        www.github.com/megamsys/seru
*/
func setHostName(name string, ip string) error {

	s := make([]string, 4)
	s = strings.Split(name, ".")

	accesskey, _ := config.GetString("aws:accesskey")
	secretkey, _ := config.GetString("aws:secretkey")

	seru := &main.NewSubdomain{
		Accesskey: accesskey,
		Secretid:  secretkey,
		Domain:    fmt.Sprint(s[1], ".", s[2], "."),
		Subdomain: s[0],
		Ip:        ip,
	}

	seruerr := seru.ApiRun(&cmd.Context{})
	if seruerr != nil {
		log.Error("Failed to seru run : %s", seruerr)
	}

	return nil
}
