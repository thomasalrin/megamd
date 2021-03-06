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
package megam

import (
	log "code.google.com/p/log4go"
	"github.com/megamsys/megamd/iaas"
	"github.com/megamsys/megamd/global"
	"github.com/tsuru/config"
	"encoding/json"
	"bytes"
	"fmt"
	"strings"
)

/*
* Register the megam provider to iaas interface
*/
func Init() {
	iaas.RegisterIaasProvider("megam", &MegamIaaS{})
}

type MegamIaaS struct{}

/*
* create the machine into megam server using knife opennebula plugin
*/
func (i *MegamIaaS) CreateMachine(pdc *global.PredefClouds, assembly *global.AssemblyWithComponents, act_id string) (string, error) {
  log.Info("Megam provider create entry")
  accesskey, err_accesskey := config.GetString("opennebula:access_key")
	if err_accesskey != nil {
		return "", err_accesskey
	}
	
	secretkey, err_secretkey := config.GetString("opennebula:secret_key")
	if err_secretkey != nil {
		return "", err_secretkey
	}
	
	str, err := buildCommand(assembly)
	if err != nil {
		return "", err
	}
	
	pair, perr := global.ParseKeyValuePair(assembly.Inputs, "domain")
	if perr != nil {
		log.Error("Failed to get the domain value : %s", perr)
	}
		
	knifePath, kerr := config.GetString("knife:path")
	if kerr != nil {
		return "", kerr
	}	
	
	str = str + " -c " + knifePath
	str = str + " -N " + assembly.Name + "." + pair.Value
	str = str + " -A " + accesskey
	str = str + " -K " + secretkey
	
	recipe, err_recipe := config.GetString("knife:recipe")
	if err_recipe != nil {
		return "", err_recipe
	}
	
	riakHost, err_riakHost := config.GetString("launch:riak")
	if err_riakHost != nil {
		return "", err_riakHost
	}
	
	rabbitmqHost, err_rabbitmq := config.GetString("launch:rabbitmq")
	if err_rabbitmq != nil {
		return "", err_rabbitmq
	}
	
	monitor, err_monitor := config.GetString("launch:monitor")
	if err_monitor != nil {
		return "", err_monitor
	}
	
	kibana, err_kibana := config.GetString("launch:kibana")
	if err_kibana != nil {
		return "", err_kibana
	}
	
	etcdHost, err_etcd := config.GetString("launch:etcd")
	if err_etcd != nil {
		return "", err_etcd
	}

	
	str = str + " --run-list recipe[" + recipe + "]"
	attributes := &iaas.Attributes{RiakHost: riakHost, AccountID: act_id, AssemblyID: assembly.Id, RabbitMQ: rabbitmqHost, MonitorHost: monitor, KibanaHost: kibana, EtcdHost: etcdHost}
    b, aerr := json.Marshal(attributes)
    if aerr != nil {        
        return "", aerr
    }
	str = str + " --json-attributes " + string(b)
	
	return str, nil
 
}

/*
* delete the machine from megam server using knife opennebula plugin
*/
func (i *MegamIaaS) DeleteMachine(pdc *global.PredefClouds, assembly *global.AssemblyWithComponents) (string, error) {
  
	accesskey, err_accesskey := config.GetString("opennebula:access_key")
	if err_accesskey != nil {
		return "", err_accesskey
	}
	
	secretkey, err_secretkey := config.GetString("opennebula:secret_key")
	if err_secretkey != nil {
		return "", err_secretkey
	}
     
     str, err := buildDelCommand(iaas.GetPlugins("opennebula"), pdc, "delete")
	if err != nil {
	return "", err
	 }
	//str = str + " -P " + " -y "
	pair, perr := global.ParseKeyValuePair(assembly.Inputs, "domain")
		if perr != nil {
			log.Error("Failed to get the domain value : %s", perr)
		}
	str = str + " -N " + assembly.Name + "." + pair.Value
	str = str + " -A " + accesskey
	str = str + " -K " + secretkey

   knifePath, kerr := config.GetString("knife:path")
	if kerr != nil {
		return "", kerr
	}
	str = strings.Replace(str, " -c ", " -c "+knifePath+" ", -1)
	str = strings.Replace(str, "<node_name>", assembly.Name + "." + pair.Value, -1 )    

    return str, nil	
}

/*
* Build the knife opennebula server create command
*/
func buildCommand(assembly *global.AssemblyWithComponents) (string, error) {
	var buffer bytes.Buffer
	buffer.WriteString("knife ")
	buffer.WriteString("opennebula ")
	buffer.WriteString("server ")
	buffer.WriteString("create")	
	cpu, perr := global.ParseKeyValuePair(assembly.Inputs, "cpu")
	ram, perr := global.ParseKeyValuePair(assembly.Inputs, "ram")
	templatekey := ""
	if len(assembly.Components) > 0 {
	   megamtemplatekey, err_templatekey := config.GetString("opennebula:default_template_name")
		if err_templatekey != nil {
			return "", err_templatekey
		}	
		templatekey = megamtemplatekey + "_" + cpu.Value + "_" + ram.Value
	} else {
		atype := make([]string, 3)
		atype = strings.Split(assembly.ToscaType, ".")
		pair, perr := global.ParseKeyValuePair(assembly.Inputs, "version")
		if perr != nil {
			log.Error("Failed to get the version : %s", perr)
		}
    	templatekey = "megam_" + atype[2] + "_" + pair.Value + "_" + cpu.Value + "_" + ram.Value
	}
	
	if len(templatekey) > 0 {
		buffer.WriteString(" --template-name " + templatekey)
	} else {
		return "", fmt.Errorf("Template doesn't loaded")
	}

    sshuserkey, err_sshuserkey := config.GetString("opennebula:ssh_user")
	if err_sshuserkey != nil {
		return "", err_sshuserkey
	}
	if len(sshuserkey) > 0 {
		buffer.WriteString(" -x " + sshuserkey)
	} else {
		return "", fmt.Errorf("Ssh user value doesn't loaded")
	}

	identityfilekey, err_identityfilekey := config.GetString("opennebula:identity_file")
	if err_identityfilekey != nil {
		return "", err_identityfilekey
	}
	if len(identityfilekey) > 0 {
		buffer.WriteString(" --identity-file " + identityfilekey)
	} else {
		return "", fmt.Errorf("Identity file doesn't loaded")
	}

	zonekey, err_zonekey := config.GetString("opennebula:zone")
	if err_zonekey != nil {
		return "", err_zonekey
	}
	if len(zonekey) > 0 {
		buffer.WriteString(" --endpoint " + zonekey)
	} else {
		return "", fmt.Errorf("Zone doesn't loaded")
	}

	return buffer.String(), nil
}

func buildDelCommand(plugin *iaas.Plugins, pdc *global.PredefClouds, command string) (string, error) {
	var buffer bytes.Buffer
	if len(plugin.Tool) > 0 {
		buffer.WriteString(plugin.Tool)
	} else {
		return "", fmt.Errorf("Plugin tool doesn't loaded")
	}
	if command == "delete" {
		if len(plugin.Command.Delete) > 0 {
			buffer.WriteString(" " + plugin.Command.Delete)
		} else {
			return "", fmt.Errorf("Plugin commands doesn't loaded")
		}
	}
	
	zonekey, err_zonekey := config.GetString("opennebula:zone")
	if err_zonekey != nil {
		return "", err_zonekey
	}
	if len(zonekey) > 0 {
		buffer.WriteString(" --endpoint " + zonekey)
	} else {
		return "", fmt.Errorf("Zone doesn't loaded")
	}
	
	return buffer.String(), nil 
	
}	
