package terraformtest

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/tidwall/gjson"
)

// TFPlan is a struct containing the terraform plan data
type TFPlan struct {
	CurDepth, MaxDepth          int
	CurItemIndex, CurItemSubKey string
	Data                        []byte
	Items                       map[string]TFResultResource
}

// TFDiff is a struct containing slice of TFDiffItem
type TFDiff struct {
	Items []TFDiffItem
}

// TFDiffItem is a struct containing got, key and want values for the diff
type TFDiffItem struct {
	Got, Key, Want string
}

// TFTestResource is a struct with values to be checked and JSON query filter
type TFTestResource struct {
	Address  string
	Metadata map[string]string
	Values   map[string]string
}

// TFResultResource is a map to store the Metadata and Values items to make easier to find resource items.
type TFResultResource map[string]map[string]gjson.Result

// New instantiate a new TFPlan object and returns a pointer to it.
func New(planPath string) (*TFPlan, error) {
	tfp := &TFPlan{
		Items:    map[string]TFResultResource{},
		MaxDepth: 10,
	}

	f, err := os.Open(planPath)
	if err != nil {
		return tfp, fmt.Errorf("cannot open file: %s", planPath)
	}
	reader := bufio.NewReader(f)
	plan, err := ioutil.ReadAll(reader)
	if err != nil {
		return tfp, fmt.Errorf("cannot read data from IO Reader: %v", err)
	}

	tfp.Data = plan
	tfp.Coalesce()

	return tfp, nil
}

// Coalesce transform the multi level json into one big object to make queries easier
func (tfPlan *TFPlan) Coalesce() {
	rootModule := gjson.GetBytes(tfPlan.Data, `planned_values.root_module|@pretty:{"sortKeys":true}`)
	rootModule.ForEach(tfPlan.coalescePlan)
}

func (tfPlan *TFPlan) coalescePlan(key, value gjson.Result) bool {
	if tfPlan.CurDepth > tfPlan.MaxDepth {
		fmt.Println("MaxDepth reached")
		return false
	}

	switch key.String() {
	case "resources":
		tfPlan.CurDepth++
		for _, child := range value.Array() {
			child.ForEach(tfPlan.coalescePlan)
		}
	case "child_modules":
		tfPlan.CurDepth++
		for _, child := range value.Array() {
			child.ForEach(tfPlan.coalescePlan)
		}
	case "values":
		tfPlan.CurItemSubKey = "Values"
		_, ok := tfPlan.Items[tfPlan.CurItemSubKey]
		if !ok {
			tfPlan.Items[tfPlan.CurItemSubKey] = map[string]map[string]gjson.Result{}
		}
		tfPlan.Items[tfPlan.CurItemSubKey][tfPlan.CurItemIndex] = map[string]gjson.Result{}
		value.ForEach(tfPlan.coalescePlan)
	default:
		if key.String() == "address" {
			tfPlan.CurItemSubKey = "Metadata"
			tfPlan.CurItemIndex = value.String()
			_, ok := tfPlan.Items[tfPlan.CurItemSubKey]
			if !ok {
				tfPlan.Items[tfPlan.CurItemSubKey] = map[string]map[string]gjson.Result{}
			}
			tfPlan.Items[tfPlan.CurItemSubKey][tfPlan.CurItemIndex] = map[string]gjson.Result{}
			break
		}
		tfPlan.Items[tfPlan.CurItemSubKey][tfPlan.CurItemIndex][key.String()] = value
		//fmt.Printf("Add key %v and value %v into %v into %v\n\n", key, value, tfPlan.CurItemIndex, tfPlan.CurItemSubKey)
	}

	return true
}

// Equal evaluate TFPlan and TFTestResource and returns the diff and if it is equal
// or not.
func Equal(tfTestResource TFTestResource, tfPlan TFPlan) (TFDiff, bool) {
	tfDiff := TFDiff{}
	resource, ok := tfPlan.Items["Metadata"][tfTestResource.Address]
	if !ok {
		tfDiffItem := TFDiffItem{
			Got:  "does not exist",
			Key:  tfTestResource.Address,
			Want: "exist",
		}
		tfDiff.Items = append(tfDiff.Items, tfDiffItem)

		return tfDiff, false
	}
	for k, v := range tfTestResource.Metadata {
		value, ok := resource[k]
		if !ok {
			tfDiffItem := TFDiffItem{
				Got:  "",
				Key:  k,
				Want: v,
			}
			tfDiff.Items = append(tfDiff.Items, tfDiffItem)

			return tfDiff, false
		}
		if value.String() != v {
			tfDiffItem := TFDiffItem{
				Got:  value.String(),
				Key:  k,
				Want: v,
			}
			tfDiff.Items = append(tfDiff.Items, tfDiffItem)

			return tfDiff, false
		}
	}

	return tfDiff, true
}

// Diff returns all diffs in a string concanated by new line
func Diff(tfDiff TFDiff) string {
	var stringDiff string
	for _, diff := range tfDiff.Items {
		stringDiff += fmt.Sprintf("key %q: want %q, got %q\n", diff.Key, diff.Want, diff.Got)
	}
	return stringDiff
}
