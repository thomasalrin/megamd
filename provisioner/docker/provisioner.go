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
	"strconv"

	log "code.google.com/p/log4go"
	"github.com/fsouza/go-dockerclient"
	"github.com/megamsys/megamd/global"
	"github.com/megamsys/megamd/provisioner"
	"github.com/tsuru/config"
)

/*
*
* Registers docker as provisioner in provisioner interface.
*
 */

func Init() {
	provisioner.Register("docker", &Docker{})
}

type Docker struct{}

const BAREMETAL = "baremetal"

/*
* Create provisioner is called to launch docker containers by
* talking to swarm cluster. Common provisioner for both
* Baremetal and VM-docker launch. Specify endpoint
* Swarm Host IP is added into the conf file.
*
 */

func (i *Docker) Create(assembly *global.AssemblyWithComponents, id string, instance bool, act_id string) (string, error) {
	log.Info("%q", assembly)
	pair_endpoint, perrscm := global.ParseKeyValuePair(assembly.Inputs, "endpoint")
	if perrscm != nil {
		log.Error("Failed to get the endpoint value : %s", perrscm)
		return "", perrscm
	}

	var endpoint string
	if pair_endpoint.Value == BAREMETAL {

		/*
		 * swarm host is obtained from conf file. Swarm host is considered
		 * only when the 'endpoint' is baremetal in the Component JSON
		 */
		api_host, _ := config.GetString("docker:swarm_host")
		endpoint = api_host

		containerID, containerName, cerr := create(assembly, endpoint)
		if cerr != nil {
			log.Error("container creation was failed : %s", cerr)
			return "", cerr
		}

		pair_cpu, perrscm := global.ParseKeyValuePair(assembly.Components[0].Inputs, "cpu")
		if perrscm != nil {
			log.Error("Failed to get the endpoint value : %s", perrscm)
		}

		pair_memory, iderr := global.ParseKeyValuePair(assembly.Components[0].Outputs, "memory")
		if iderr != nil {
			log.Error("Failed to get the endpoint value : %s", iderr)
		}

		serr := StartContainer(containerID, endpoint, pair_cpu.Value, pair_memory.Value)
		if serr != nil {
			log.Error("container starting error : %s", serr)
			return "", serr
		}

		ipaddress, iperr := setContainerNAL(containerID, containerName, endpoint)
		if iperr != nil {
			log.Error("set container network was failed : %s", iperr)
			return "", iperr
		}

		herr := setHostName(containerName, ipaddress)
		if herr != nil {
			log.Error("set host name error : %s", herr)
		}

		updateContainerJSON(assembly, ipaddress, containerID, endpoint)
	} else {
		endpoint = pair_endpoint.Value
		create(assembly, endpoint)
	}

	return "", nil
}

/*
* Delete command kills the container by talking to swarm cluster and giving
* the container ID.
*
 */
func (i *Docker) Delete(assembly *global.AssemblyWithComponents, id string) (string, error) {

	pair_endpoint, perrscm := global.ParseKeyValuePair(assembly.Inputs, "endpoint")
	if perrscm != nil {
		log.Error("Failed to get the endpoint value : %s", perrscm)
	}

	pair_id, iderr := global.ParseKeyValuePair(assembly.Components[0].Outputs, "id")
	if iderr != nil {
		log.Error("Failed to get the endpoint value : %s", iderr)
	}

	var endpoint string
	if pair_endpoint.Value == BAREMETAL {

		api_host, _ := config.GetString("docker:swarm_host")
		endpoint = api_host

	} else {
		endpoint = pair_endpoint.Value
	}

	client, _ := docker.NewClient(endpoint)
	kerr := client.KillContainer(docker.KillContainerOptions{ID: pair_id.Value})
	if kerr != nil {
		log.Error("Failed to kill the container : %s", kerr)
		return "", kerr
	}
	log.Info("Container is killed")
	return "", nil
}

/*
* Docker API client to connect to swarm/docker VM.
* Swarm supports all docker API endpoints
 */
func create(assembly *global.AssemblyWithComponents, endpoint string) (string, string, error) {

	pair_img, perrscm := global.ParseKeyValuePair(assembly.Components[0].Inputs, "source")
	if perrscm != nil {
		log.Error("Failed to get the image value : %s", perrscm)
		return "", "", perrscm
	}

	pair_domain, perrdomain := global.ParseKeyValuePair(assembly.Components[0].Inputs, "domain")
	if perrdomain != nil {
		log.Error("Failed to get the image value : %s", perrdomain)
		return "", "", perrdomain
	}

	client, _ := docker.NewClient(endpoint)

	opts := docker.PullImageOptions{
		Repository: pair_img.Value,
	}
	pullerr := client.PullImage(opts, docker.AuthConfiguration{})
	if pullerr != nil {
		log.Error("Image pulled failed : %s", pullerr)
		return "", "", pullerr
	}

	dconfig := docker.Config{Image: pair_img.Value, NetworkDisabled: true}
	copts := docker.CreateContainerOptions{Name: fmt.Sprint(assembly.Components[0].Name, ".", pair_domain.Value), Config: &dconfig}

	/*
	 * Creation of the container with copts.
	 */

	container, conerr := client.CreateContainer(copts)
	if conerr != nil {
		log.Error("Container creation failed : %s", conerr)
		return "", "", conerr
	}

	cont := &docker.Container{}
	mapP, _ := json.Marshal(container)
	json.Unmarshal([]byte(string(mapP)), cont)

	return cont.ID, cont.Name, nil
}

/*
* start the container using docker endpoint
 */
func StartContainer(containerID string, endpoint string, cpu string, cmemory string) error {

	client, _ := docker.NewClient(endpoint)

	/*
	 * hostConfig{} struct for portbindings - to expose visible ports
	 *  Also for specifying the container configurations (memory, cpuquota etc)
	 */
	mem, _ := strconv.Atoi(cmemory)
	var memory int64
	memory = int64(mem)

	cpuq, _ := strconv.Atoi(cpu)
	var cpuqo int64
	cpuqo = int64(cpuq)
	cpuQuota := cpuqo * 25000

	period := 50000
	var cpuPeriod int64
	cpuPeriod = int64(period)

	hostConfig := docker.HostConfig{Memory: memory, CPUPeriod: cpuPeriod, CPUQuota: cpuQuota}

	/*
	 *   Starting container once the container is created - container ID &
	 *   hostConfig is provided to start the container.
	 *
	 */
	serr := client.StartContainer(containerID, &hostConfig)
	if serr != nil {
		log.Error("Start container was failed : %s", serr)
		return serr
	}
	return nil
}

/*
* stop the container using docker endpoint
 */
func StopContainer(containerID string, endpoint string) error {

	client, _ := docker.NewClient(endpoint)
	serr := client.StopContainer(containerID, 10)
	if serr != nil {
		log.Error("container was not stopped - Error : %s", serr)
		return serr
	}
	return nil
}

/*
* restart the container using docker endpoint
 */
func RestartContainer(containerID string, endpoint string) error {
	client, _ := docker.NewClient(endpoint)
	rerr := client.RestartContainer(containerID, 10)
	if rerr != nil {
		log.Error("container was not restarted - Error : %s", rerr)
		return rerr
	}
	return nil
}
