package common

// RatioValue mirrors capcut_business_common.Ratio. Custom (1) has no CLI
// width/height parameters, so it and unknown enums have no display value.
func RatioValue(value int64) string {
	return ratioValues[value]
}

var ratioValues = map[int64]string{
	0: "adaptive", 2: "16:9", 3: "9:16", 4: "4:3", 5: "3:4", 6: "1:1",
	7: "2:1", 8: "2.35:1", 9: "1.85:1", 10: "1.125:2.436", 11: "3:2", 12: "2:3", 13: "21:9",
}
