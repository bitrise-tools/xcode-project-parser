package xcodeproj

import (
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bitrise-io/go-utils/fileutil"
	"github.com/bitrise-io/go-utils/pathutil"
	"github.com/bitrise-io/xcode-project/serialized"
	"github.com/bitrise-io/xcode-project/xcodebuild"
	"github.com/bitrise-io/xcode-project/xcscheme"
	"golang.org/x/text/unicode/norm"
	"howett.net/plist"
)

// XcodeProj ...
type XcodeProj struct {
	Proj    Proj
	RawProj serialized.Object
	Format  int

	Name string
	Path string
}

func (p XcodeProj) buildSettingsFilePath(target, configuration, key string) (string, error) {
	buildSettings, err := p.TargetBuildSettings(target, configuration)
	if err != nil {
		return "", err
	}

	pth, err := buildSettings.String(key)
	if err != nil {
		return "", err
	}

	if pathutil.IsRelativePath(pth) {
		pth = filepath.Join(filepath.Dir(p.Path), pth)
	}

	return pth, nil
}

// TargetCodeSignEntitlementsPath ...
func (p XcodeProj) TargetCodeSignEntitlementsPath(target, configuration string) (string, error) {
	return p.buildSettingsFilePath(target, configuration, "CODE_SIGN_ENTITLEMENTS")
}

// TargetCodeSignEntitlements ...
func (p XcodeProj) TargetCodeSignEntitlements(target, configuration string) (serialized.Object, error) {
	codeSignEntitlementsPth, err := p.TargetCodeSignEntitlementsPath(target, configuration)
	if err != nil {
		return nil, err
	}

	codeSignEntitlementsContent, err := fileutil.ReadBytesFromFile(codeSignEntitlementsPth)
	if err != nil {
		return nil, err
	}

	var codeSignEntitlements serialized.Object
	if _, err := plist.Unmarshal([]byte(codeSignEntitlementsContent), &codeSignEntitlements); err != nil {
		return nil, err
	}

	return codeSignEntitlements, nil
}

// TargetInformationPropertyListPath ...
func (p XcodeProj) TargetInformationPropertyListPath(target, configuration string) (string, error) {
	return p.buildSettingsFilePath(target, configuration, "INFOPLIST_FILE")
}

// TargetInformationPropertyList ...
func (p XcodeProj) TargetInformationPropertyList(target, configuration string) (serialized.Object, error) {
	informationPropertyListPth, err := p.TargetInformationPropertyListPath(target, configuration)
	if err != nil {
		return nil, err
	}

	informationPropertyListContent, err := fileutil.ReadBytesFromFile(informationPropertyListPth)
	if err != nil {
		return nil, err
	}

	var informationPropertyList serialized.Object
	if _, err := plist.Unmarshal([]byte(informationPropertyListContent), &informationPropertyList); err != nil {
		return nil, err
	}

	return informationPropertyList, nil
}

// TargetBundleID ...
func (p XcodeProj) TargetBundleID(target, configuration string) (string, error) {
	buildSettings, err := p.TargetBuildSettings(target, configuration)
	if err != nil {
		return "", err
	}

	bundleID, err := buildSettings.String("PRODUCT_BUNDLE_IDENTIFIER")
	if err != nil && !serialized.IsKeyNotFoundError(err) {
		return "", err
	}

	if bundleID != "" {
		return resolve(bundleID, buildSettings)
	}

	informationPropertyList, err := p.TargetInformationPropertyList(target, configuration)
	if err != nil {
		return "", err
	}

	bundleID, err = informationPropertyList.String("CFBundleIdentifier")
	if err != nil {
		return "", err
	}

	if bundleID == "" {
		return "", errors.New("no PRODUCT_BUNDLE_IDENTIFIER build settings nor CFBundleIdentifier information property found")
	}

	return resolve(bundleID, buildSettings)
}

func resolve(bundleID string, buildSettings serialized.Object) (string, error) {
	resolvedBundleIDs := map[string]bool{}
	resolved := bundleID
	for true {
		var err error
		resolved, err = expand(resolved, buildSettings)
		if err != nil {
			return "", err
		}

		if !strings.Contains(resolved, "$") {
			return resolved, nil
		}

		_, ok := resolvedBundleIDs[resolved]
		if ok {
			return "", fmt.Errorf("bundle id reference cycle found")
		}
		resolvedBundleIDs[resolved] = true
	}
	return "", fmt.Errorf("failed to resolve bundle id: %s", bundleID)
}

func expand(bundleID string, buildSettings serialized.Object) (string, error) {
	if !strings.Contains(bundleID, "$") {
		return bundleID, nil
	}

	pattern := `(.*)\$\((.*)\)(.*)`
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(bundleID)
	if len(match) != 4 {
		return "", fmt.Errorf("%s does not match to pattern: %s", bundleID, pattern)
	}

	prefix := match[1]
	suffix := match[3]
	envKey := match[2]

	split := strings.Split(envKey, ":")
	envKey = split[0]

	envValue, err := buildSettings.String(envKey)
	if err != nil {
		if serialized.IsKeyNotFoundError(err) {
			return "", fmt.Errorf("%s build settings not found", envKey)
		}
		return "", err
	}

	return prefix + envValue + suffix, nil
}

// TargetBuildSettings ...
func (p XcodeProj) TargetBuildSettings(target, configuration string, customOptions ...string) (serialized.Object, error) {
	return xcodebuild.ShowProjectBuildSettings(p.Path, target, configuration, customOptions...)
}

// Scheme ...
func (p XcodeProj) Scheme(name string) (xcscheme.Scheme, bool) {
	schemes, err := p.Schemes()
	if err != nil {
		return xcscheme.Scheme{}, false
	}

	normName := norm.NFC.String(name)
	for _, scheme := range schemes {
		if norm.NFC.String(scheme.Name) == normName {
			return scheme, true
		}
	}

	return xcscheme.Scheme{}, false
}

// Schemes ...
func (p XcodeProj) Schemes() ([]xcscheme.Scheme, error) {
	return xcscheme.FindSchemesIn(p.Path)
}

// Open ...
func Open(pth string) (XcodeProj, error) {
	absPth, err := pathutil.AbsPath(pth)
	if err != nil {
		return XcodeProj{}, err
	}

	raw, objects, projectID, format, err := open(pth)

	p, err := parseProj(projectID, objects)
	if err != nil {
		return XcodeProj{}, err
	}

	return XcodeProj{
		Proj:    p,
		RawProj: raw,
		Format:  format,
		Path:    absPth,
		Name:    strings.TrimSuffix(filepath.Base(absPth), filepath.Ext(absPth)),
	}, nil
}

// open parse the provided .pbxprog file.
// Returns the `raw` contents as a serialized.Object, the `objects` as serialized.Object and the PBXProject's `projectID` as string
// If there was an error during the parsing it returns an error
func open(absPth string) (rawPbxProj serialized.Object, objects serialized.Object, projectID string, format int, err error) {
	pbxProjPth := filepath.Join(absPth, "project.pbxproj")

	var b []byte
	b, err = fileutil.ReadBytesFromFile(pbxProjPth)
	if err != nil {
		return
	}

	if format, err = plist.Unmarshal(b, &rawPbxProj); err != nil {
		err = fmt.Errorf("failed to generate json from Pbxproj - error: %s", err)
		return
	}

	objects, err = rawPbxProj.Object("objects")
	if err != nil {
		return serialized.Object{}, serialized.Object{}, "", 0, err
	}

	for id := range objects {
		var object serialized.Object
		object, err = objects.Object(id)
		if err != nil {
			return
		}

		var objectISA string
		objectISA, err = object.String("isa")
		if err != nil {
			return
		}

		if objectISA == "PBXProject" {
			projectID = id
			break
		}
	}
	return
}

// IsXcodeProj ...
func IsXcodeProj(pth string) bool {
	return filepath.Ext(pth) == ".xcodeproj"
}

// ForceCodeSign modifies the project's code signing settings to use manual code signing.
//
// Overrides the target's `ProvisioningStyle`, `DevelopmentTeam` and clears the `DevelopmentTeamName` in the **TargetAttributes**.
// Overrides the target's `CODE_SIGN_STYLE`, `DEVELOPMENT_TEAM`, `CODE_SIGN_IDENTITY`, `CODE_SIGN_IDENTITY[sdk=iphoneos*]` `PROVISIONING_PROFILE_SPECIFIER`,
// `PROVISIONING_PROFILE` and `PROVISIONING_PROFILE[sdk=iphoneos*]` in the **BuildSettings**.
func (p *XcodeProj) ForceCodeSign(targetName, developmentTeam, codesignIdentity, provisioningProfileUUID string) error {
	targetAttributes, err := p.TargetAttributes()
	if err != nil {
		return fmt.Errorf("failed to get project's target attributes, error: %s", err)
	}

	target, ok := p.Proj.TargetByName(targetName)
	if !ok {
		return fmt.Errorf("failed to find target with name: %s", targetName)
	}

	// Override TargetAttributes
	if err = foreceCodeSignOnTargetAttributes(targetAttributes, target.ID, developmentTeam); err != nil {
		return fmt.Errorf("failed to change code signing in target attributes, error: %s", err)
	}

	// Override BuildSettings
	if err = foreceCodeSignOnBuildSettings(target.ID, developmentTeam, provisioningProfileUUID); err != nil {
		return fmt.Errorf("failed to change code signing in build settings, error: %s", err)
	}
	return nil
}

// foreceCodeSignOnTargetAttributes sets the TargetAttributes for the provided targetID.
// **Overrides the ProvisioningStyle, developmentTeam and clears the DevelopmentTeamName in the provided `targetAttributes`!**
func foreceCodeSignOnTargetAttributes(targetAttributes serialized.Object, targetID, developmentTeam string) error {
	targetAttribute, err := targetAttributes.Object(targetID)
	if err != nil {
		return fmt.Errorf("failed to get traget's (%s) attributes, error: %s", targetID, err)
	}

	targetAttribute["ProvisioningStyle"] = "Manual"
	targetAttribute["DevelopmentTeam"] = developmentTeam
	targetAttribute["DevelopmentTeamName"] = ""
	return nil
}

func foreceCodeSignOnBuildSettings(targetID, developmentTeam, provisioningProfileUUID string) error {
	return nil
}

// Save the XcodeProj
//
// Overrides the project.pbxproj file of the XcodeProj with the contents of `rawProj`
func (p XcodeProj) Save() error {
	return p.savePBXProj()
}

// savePBXProj overrides the project.pbxproj file of the XcodeProj with the contents of `rawProj`
func (p XcodeProj) savePBXProj() error {
	b, err := plist.Marshal(p.RawProj, p.Format)
	if err != nil {
		return fmt.Errorf("failed to marshal .pbxproj")
	}

	pth := path.Join(p.Path, "project.pbxproj")
	return ioutil.WriteFile(pth, b, 0644)
}
