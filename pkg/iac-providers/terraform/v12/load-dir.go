package tfv12

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	version "github.com/hashicorp/go-version"
	"github.com/hashicorp/hcl/v2"
	hclConfigs "github.com/hashicorp/terraform/configs"
	"github.com/spf13/afero"

	"github.com/accurics/terrascan/pkg/iac-providers/output"
	"github.com/accurics/terrascan/pkg/utils"
)

// LoadIacDir starts traversing from the given rootDir and traverses through
// all the descendant modules present to create an output list of all the
// resources present in rootDir and descendant modules
func (*TfV12) LoadIacDir(rootDir string) (allResourcesConfig output.AllResourceConfigs, err error) {

	// get absolute path
	absRootDir, err := utils.GetAbsPath(rootDir)
	if err != nil {
		return allResourcesConfig, err
	}

	// create a new config parser
	parser := hclConfigs.NewParser(afero.NewOsFs())

	// check if the directory has any tf config files (.tf or .tf.json)
	if !parser.IsConfigDir(absRootDir) {
		errMsg := fmt.Sprintf("directory '%s' has no terraform config files")
		log.Printf(errMsg)
		return allResourcesConfig, fmt.Errorf(errMsg)
	}

	// load root config directory
	rootMod, diags := parser.LoadConfigDir(absRootDir)
	if diags.HasErrors() {
		log.Printf("failed to load terraform config dir '%s'. error:\n%+v\n", rootDir, diags)
		return allResourcesConfig, fmt.Errorf("failed to load terraform allResourcesConfig dir")
	}

	// using the BuildConfig and ModuleWalkerFunc to traverse through all
	// descendant modules from the root module and create a unified
	// configuration of type *configs.Config
	// Note: currently, only Local paths are supported for Module Sources
	versionI := 0
	unified, diags := hclConfigs.BuildConfig(rootMod, hclConfigs.ModuleWalkerFunc(
		func(req *hclConfigs.ModuleRequest) (*hclConfigs.Module, *version.Version, hcl.Diagnostics) {

			// Note: currently only local paths are supported for Module Sources

			// determine the absolute path from root module to the sub module
			// using *configs.ModuleRequest.Path field
			var (
				pathArr      = strings.Split(req.Path.String(), ".")
				pathToModule = absRootDir
			)
			for _, subPath := range pathArr {
				pathToModule = filepath.Join(pathToModule, subPath)
			}

			// load sub module directory
			subMod, diags := parser.LoadConfigDir(pathToModule)
			version, _ := version.NewVersion(fmt.Sprintf("1.0.%d", versionI))
			versionI++
			return subMod, version, diags
		},
	))
	if diags.HasErrors() {
		log.Printf("failed to build unified config. errors:\n%+v\n", diags)
		return allResourcesConfig, fmt.Errorf("failed to build terraform allResourcesConfig")
	}

	/*
		The "unified" config created from BuildConfig in the previous step
		represents a tree structure with rootDir module being at its root and
		all the sub modules being its children, and these children can have
		more children and so on...

		Now, using BFS we traverse through all the submodules using the classic
		approach of using a queue data structure
	*/

	// queue of for BFS, add root module config to it
	configsQ := []*hclConfigs.Config{unified.Root}

	// using BFS traverse through all modules in the unified config tree
	for len(configsQ) > 0 {

		// pop first element from the queue
		current := configsQ[0]
		configsQ = configsQ[1:]

		// traverse through all current's resources
		for _, managedResource := range current.Module.ManagedResources {

			// create output.ResourceConfig from hclConfigs.Resource
			resourceConfig, err := CreateResourceConfig(managedResource)
			if err != nil {
				return allResourcesConfig, fmt.Errorf("failed to create ResourceConfig")
			}

			// append resource config to list of all resources
			allResourcesConfig = append(allResourcesConfig, resourceConfig)
		}

		// add all current's children to the queue
		for _, childModule := range current.Children {
			configsQ = append(configsQ, childModule)
		}
	}

	// successful
	return allResourcesConfig, nil
}