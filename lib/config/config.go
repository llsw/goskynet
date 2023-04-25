package config

import (
	"io/ioutil"
	"os"
	"reflect"
	"sync"
	"time"

	// "gopkg.in/yaml.v2"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gopkg.in/yaml.v3"
)

var lock = &sync.Mutex{}

type PprofConifg struct {
	Address string `yaml:"address"`
}

type LogConfig struct {
	Path     string     `yaml:"path"`
	Level    hlog.Level `yaml:"level"`
	Interval int64      `yaml:"interval"`
}
type Config map[string]interface{}

type ConfigStruct struct {
	// 定义你的配置项
	Name        string       `yaml:"name"`
	Address     string       `yaml:"address"`
	Version     string       `yaml:"version"`
	Workers     int          `yaml:"workers"`
	Log         *LogConfig   `yaml:"log"`
	Pprof       *PprofConifg `yaml:"pprof"`
	Clustername string       `yaml:"clustername"`
	Extend      string       `yaml:"extend"`
	Config      *Config      `yaml:"config"`
}

type ConfigReader struct {
	configFilePath string
	lastModified   time.Time
	config         *Config
	mutex          sync.Mutex
}

type ConfigReaderStruct struct {
	configFilePath     string
	lastModified       time.Time
	lastExtendModified time.Time
	config             *ConfigStruct
	mutex              sync.Mutex
}

func NewConfigReader(configFilePath string) *ConfigReader {
	return &ConfigReader{
		configFilePath: configFilePath,
		lastModified:   time.Time{},
		config:         nil,
		mutex:          sync.Mutex{},
	}
}

func NewConfigReaderStruct(configFilePath string) *ConfigReaderStruct {
	return &ConfigReaderStruct{
		configFilePath: configFilePath,
		lastModified:   time.Time{},
		config:         nil,
		mutex:          sync.Mutex{},
	}
}

func (c *ConfigReader) loadConfig() error {
	data, err := ioutil.ReadFile(c.configFilePath)
	if err != nil {
		return err
	}

	config := &Config{}
	err = yaml.Unmarshal(data, config)
	if err != nil {
		return err
	}

	c.config = config
	c.lastModified = time.Now()

	return nil
}

func loadExtend(c *ConfigReaderStruct) error {
	if c.config.Extend != "" {
		data, err := ioutil.ReadFile(c.config.Extend)
		if err != nil {
			return err
		}

		extend := Config{}
		err = yaml.Unmarshal(data, &extend)
		if err != nil {
			return err
		}

		c.config.Config = &extend
		c.lastExtendModified = time.Now()
	}
	return nil
}

func (c *ConfigReaderStruct) loadConfigStruct() error {
	data, err := ioutil.ReadFile(c.configFilePath)
	if err != nil {
		return err
	}

	config := &ConfigStruct{}
	err = yaml.Unmarshal(data, config)
	if err != nil {
		return err
	}

	c.config = config

	loadExtend(c)

	c.lastModified = time.Now()

	return nil
}

func (c *ConfigReader) getConfig() (*Config, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	fileInfo, err := os.Stat(c.configFilePath)
	if err != nil {
		return nil, err
	}

	if c.config == nil || fileInfo.ModTime().After(c.lastModified) {
		oldConfig := c.config

		err = c.loadConfig()
		if err != nil {
			return nil, err
		}

		if oldConfig != nil {
			c.compareConfigs(oldConfig, c.config)
		}
	}

	return c.config, nil
}

func (c *ConfigReaderStruct) getConfigStruct() (*ConfigStruct, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	fileInfo, err := os.Stat(c.configFilePath)
	if err != nil {
		return nil, err
	}

	if c.config == nil || fileInfo.ModTime().After(c.lastModified) {
		oldConfig := c.config

		err = c.loadConfigStruct()
		if err != nil {
			return nil, err
		}

		if oldConfig != nil {
			// 比较新旧配置
			c.compareConfigsStruct(oldConfig, c.config)
		}
	}

	if c.config != nil && c.config.Extend != "" {
		fileInfo, err = os.Stat(c.config.Extend)
		if err != nil {
			return nil, err
		}

		if fileInfo.ModTime().After(c.lastExtendModified) {
			oldExtend := c.config.Config
			err = loadExtend(c)
			if err == nil {
				if oldExtend != nil {
					// 比较新旧配置
					c.compareConfigs(oldExtend, c.config.Config)
				}
			}
		}
	}

	return c.config, nil
}

func (c *ConfigReader) GetConfig() *Config {
	return c.config
}

func (c *ConfigReaderStruct) GetConfigStruct() *ConfigStruct {
	return c.config
}

func (c *ConfigReaderStruct) compareConfigsStruct(oldConfig *ConfigStruct,
	newConfig *ConfigStruct) {

	ov := reflect.ValueOf(oldConfig).Elem()
	nv := reflect.ValueOf(newConfig).Elem()

	for i := 0; i < ov.NumField(); i++ {
		ofv := ov.Field(i)
		nfv := nv.Field(i)

		if !reflect.DeepEqual(ofv.Interface(), nfv.Interface()) {
			hlog.Infof(
				"configuration item '%s'"+
					"changed: old value '%v', "+
					"new value '%v'\n",
				ov.Type().Field(i).Name,
				ofv.Interface(),
				nfv.Interface(),
			)

			if ov.Type().Field(i).Name == "Extend" {
				loadExtend(c)
			}
		}
	}
}

func (c *ConfigReaderStruct) compareConfigs(oldConfig *Config,
	newConfig *Config) {
	missingKeys := make(map[string]bool)
	for key, oldValue := range *oldConfig {
		newValue, ok := (*newConfig)[key]
		if !ok {
			missingKeys[key] = true
			continue
		}
		if !reflect.DeepEqual(oldValue, newValue) {
			hlog.Infof(
				"Key %s: old value %v, new value %v\n",
				key, oldValue, newValue,
			)
		}
	}

	for key, _ := range *newConfig {
		if _, ok := (*oldConfig)[key]; !ok {
			hlog.Infof(
				"Key %s is present in "+
					"new config but not in old config\n", key,
			)
		}
	}

	for key, _ := range missingKeys {
		hlog.Infof(
			"Key %s is present in "+
				"old config but not in new config\n", key,
		)
	}
}

func (c *ConfigReader) compareConfigs(oldConfig *Config,
	newConfig *Config) {
	missingKeys := make(map[string]bool)
	for key, oldValue := range *oldConfig {
		newValue, ok := (*newConfig)[key]
		if !ok {
			missingKeys[key] = true
			continue
		}
		if !reflect.DeepEqual(oldValue, newValue) {
			hlog.Infof(
				"Key %s: old value %v, new value %v\n",
				key, oldValue, newValue,
			)
		}
	}

	for key, _ := range *newConfig {
		if _, ok := (*oldConfig)[key]; !ok {
			hlog.Infof(
				"Key %s is present in "+
					"new config but not in old config\n", key,
			)
		}
	}

	for key, _ := range missingKeys {
		hlog.Infof(
			"Key %s is present in "+
				"old config but not in new config\n", key,
		)
	}
}

func syncConfig(configReader *ConfigReader) {
	for {
		_, err := configReader.getConfig()
		if err != nil {
			hlog.Errorf("failed to read config: %v\n", err)
			continue
		}
		time.Sleep(time.Second * 60)
	}
}

func syncConfigStruct(configReader *ConfigReaderStruct) {
	for {
		_, err := configReader.getConfigStruct()
		if err != nil {
			hlog.Errorf("failed to read config: %v\n", err)
			continue
		}
		time.Sleep(time.Second * 60)
	}
}

var configs = make(map[string]*Config)
var mapOnce sync.Once

func GetInstanceMap(path string) (config *Config, err error) {
	if c, ok := configs[path]; ok {
		config = c
	} else {
		mapOnce.Do(func() {
			if c, ok := configs[path]; ok {
				config = c
			} else {
				configReader := NewConfigReader(path)
				config, err = configReader.getConfig()
				if err != nil {
					return
				}
				go syncConfig(configReader)
			}
		})
	}
	return
}

var configsStruct = make(map[string]*ConfigStruct)
var structOnce sync.Once

func GetInstanceStruct(path string) (config *ConfigStruct, err error) {
	if c, ok := configsStruct[path]; ok {
		config = c
	} else {
		structOnce.Do(func() {
			if c, ok := configsStruct[path]; ok {
				config = c
			} else {
				configReader := NewConfigReaderStruct(path)
				config, err = configReader.getConfigStruct()
				hlog.Debugf("config:%v", config)
				if err != nil {
					return
				}
				go syncConfigStruct(configReader)
			}
		})
	}
	return
}

type ClusterConfig struct {
	Path        string
	Config      *ConfigStruct
	Clustername *Config
}

var clusterConfig *ClusterConfig

func GetInstance() (config *ClusterConfig) {
	if clusterConfig != nil {
		config = clusterConfig
	} else {
		lock.Lock()
		defer lock.Unlock()
		if clusterConfig != nil {
			config = clusterConfig
		} else {
			config = &ClusterConfig{}
			clusterConfig = config
		}
	}
	return
}

func LoadClusterConfig(path string) (err error) {
	ins := GetInstance()
	ins.Path = path
	ins.Config, err = GetInstanceStruct(path)
	if err != nil {
		return err
	}
	ins.Clustername, err = GetInstanceMap(ins.Config.Clustername)
	return
}
