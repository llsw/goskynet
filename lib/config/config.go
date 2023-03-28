package config

import (
	"fmt"
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

type LogConfig struct {
	Path     string `yaml:"path"`
	Interval int    `yaml:"interval"`
}

type ConfigStruct struct {
	// 定义你的配置项
	Name        string     `yaml:"name"`
	Address     string     `yaml:"address"`
	Workers     int        `yaml:"workers"`
	Log         *LogConfig `yaml:"log"`
	Clustername string     `yaml:"clustername"`
}

type Config map[string]interface{}

type ConfigReader struct {
	configFilePath string
	lastModified   time.Time
	config         *Config
	mutex          sync.Mutex
}

type ConfigReaderStruct struct {
	configFilePath string
	lastModified   time.Time
	config         *ConfigStruct
	mutex          sync.Mutex
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
			// 比较新旧配置
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

	return c.config, nil
}

func (c *ConfigReader) GetConfig() *Config {
	return c.config
}

func (c *ConfigReaderStruct) GetConfigStruct() *ConfigStruct {
	return c.config
}

func (c *ConfigReaderStruct) compareConfigsStruct(oldConfig *ConfigStruct, newConfig *ConfigStruct) {
	oldConfigValue := reflect.ValueOf(oldConfig).Elem()
	newConfigValue := reflect.ValueOf(newConfig).Elem()

	for i := 0; i < oldConfigValue.NumField(); i++ {
		oldFieldValue := oldConfigValue.Field(i)
		newFieldValue := newConfigValue.Field(i)

		if !reflect.DeepEqual(oldFieldValue.Interface(), newFieldValue.Interface()) {
			hlog.Infof("Configuration item '%s' changed: old value '%v', new value '%v'\n",
				oldConfigValue.Type().Field(i).Name, oldFieldValue.Interface(), newFieldValue.Interface())
		}
	}
}

func (c *ConfigReader) compareConfigs(oldConfig *Config, newConfig *Config) {
	// Create a set to keep track of keys that are present in oldConfig but not in newConfig.
	missingKeys := make(map[string]bool)

	// Compare values for keys that are present in both oldConfig and newConfig.
	for key, oldValue := range *oldConfig {
		newValue, ok := (*newConfig)[key]
		if !ok {
			missingKeys[key] = true
			continue
		}
		if !reflect.DeepEqual(oldValue, newValue) {
			fmt.Printf("Key %s: old value %v, new value %v\n", key, oldValue, newValue)
		}
	}

	// Check for keys that are present in newConfig but not in oldConfig.
	for key, _ := range *newConfig {
		if _, ok := (*oldConfig)[key]; !ok {
			fmt.Printf("Key %s is present in new config but not in old config\n", key)
		}
	}

	// Print out the keys that were missing from newConfig.
	for key, _ := range missingKeys {
		fmt.Printf("Key %s is present in old config but not in new config\n", key)
	}
}

func syncConfig(configReader *ConfigReader) {
	for {
		config, err := configReader.getConfig()
		if err != nil {
			hlog.Errorf("failed to read config: %v\n", err)
			continue
		}
		hlog.Errorf("config: %v\n", config)
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
		// hlog.Errorf("config: %v\n", config)
		time.Sleep(time.Second * 60)
	}
}

var configs = make(map[string]*Config)

func GetInstanceMap(path string) (config *Config, err error) {
	if c, ok := configs[path]; ok {
		config = c
	} else {
		lock.Lock()
		defer lock.Unlock()
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
	}
	return
}

var configsStruct = make(map[string]*ConfigStruct)

func GetInstanceStruct(path string) (config *ConfigStruct, err error) {
	if c, ok := configsStruct[path]; ok {
		config = c
	} else {
		lock.Lock()
		defer lock.Unlock()
		if c, ok := configsStruct[path]; ok {
			config = c
		} else {
			configReader := NewConfigReaderStruct(path)
			config, err = configReader.getConfigStruct()
			if err != nil {
				return
			}
			go syncConfigStruct(configReader)
		}
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
