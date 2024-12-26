package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BurntSushi/toml"
	"github.com/caarlos0/env"
)

func loadDefault(defaultValues string, cfg interface{}) error {
	if _, err := toml.Decode(defaultValues, cfg); err != nil {
		return err
	}
	return nil
}

func loadFile(path string, cfg interface{}) error {
	bs, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return err
	}
	cfgToml := string(bs)
	if _, err := toml.Decode(cfgToml, cfg); err != nil {
		return err
	}
	return nil
}

func loadEnv(cfg interface{}) error {
	if err := env.Parse(cfg); err != nil {
		return err
	}
	return nil
}

// LoadConfig is the function that loads the configuration
func LoadConfig(filePath string, defaultValues string, cfg interface{}) error {
	//Get default configuration
	if err := loadDefault(defaultValues, cfg); err != nil {
		return fmt.Errorf("error loading default configuration: %w", err)
	}
	// Get file configuration
	var errLoadFile error
	if filePath != "" {
		errLoadFile = loadFile(filePath, cfg)
	}
	// Overwrite file configuration with the env configuration
	errLoadEnv := loadEnv(cfg)
	if errLoadFile != nil {
		return fmt.Errorf("error loading configuration file: %w", errLoadFile)
	}
	if errLoadEnv != nil {
		return fmt.Errorf("error loading environment variables: %w", errLoadEnv)
	}
	return nil
}

func structToMapNode(item interface{}, prevTags string) map[string]interface{} {
	res := map[string]interface{}{}
	if item == nil {
		return res
	}
	v := reflect.TypeOf(item)
	reflectValue := reflect.ValueOf(item)
	reflectValue = reflect.Indirect(reflectValue)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	for i := 0; i < v.NumField(); i++ {
		tag := v.Field(i).Name
		tag2 := v.Field(i).Tag.Get("env")
		field := reflectValue.Field(i)
		if v.Field(i).Type.Kind() == reflect.Struct {
			res[tag] = structToMapNode(field.Interface(), tag+"::"+tag2)
		} else {
			if !field.IsZero() {
				if reflect.TypeOf(field.Interface()).String() == "time.Duration" {
					res[prevTags] = reflect.ValueOf(field.Interface())
				} else {
					res[tag+"::"+tag2] = reflect.ValueOf(field.Interface())
				}
			}
		}
	}
	for k, v := range res {
		if reflect.TypeOf(v).String() == "reflect.Value" {
			log.Println("############## ", k, " ==> ", v)
		}
	}
	return res
}

func SourceParamsNode(filePath string, envCfg, fileCfg interface{}) error {
	log.Println("ENV parameters")
	errorEnv := loadEnv(envCfg)
	if errorEnv != nil {
		return fmt.Errorf("error reading environment variables: %w", errorEnv)
	}
	_ = structToMapNode(envCfg, "")

	log.Println("File parameters")
	var errorFile error
	if filePath != "" {
		errorFile = loadFile(filePath, fileCfg)
	}
	if errorFile != nil {
		return fmt.Errorf("error reading file variables: %w", errorFile)
	}
	_ = structToMapNode(fileCfg, "")
	return nil
}
