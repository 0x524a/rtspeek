package rtspeek

import (
	"fmt"
	"strings"

	h264conf "github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	h265conf "github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
)

// codecInfo is the profile, level and RFC 6381 codec string read from an SPS.
type codecInfo struct {
	Profile, Level, CodecString string
}

var h264Profiles = map[uint8]string{
	44: "CAVLC 4:4:4 Intra", 66: "Baseline", 77: "Main", 88: "Extended",
	100: "High", 110: "High 10", 122: "High 4:2:2", 244: "High 4:4:4 Predictive",
}

// h264Info describes a raw H.264 SPS, or returns nil when it does not parse.
func h264Info(sps []byte) *codecInfo {
	var s h264conf.SPS
	if s.Unmarshal(sps) != nil {
		return nil
	}
	name, ok := h264Profiles[s.ProfileIdc]
	if !ok {
		name = fmt.Sprintf("Profile %d", s.ProfileIdc)
	}
	if s.ProfileIdc == 66 && s.ConstraintSet1Flag {
		name = "Constrained Baseline"
	}
	level := fmt.Sprintf("%d.%d", s.LevelIdc/10, s.LevelIdc%10)
	if s.LevelIdc == 9 || (s.LevelIdc == 11 && s.ConstraintSet3Flag && s.ProfileIdc <= 88) {
		level = "1b"
	}
	var cs uint8
	for i, f := range []bool{s.ConstraintSet0Flag, s.ConstraintSet1Flag, s.ConstraintSet2Flag,
		s.ConstraintSet3Flag, s.ConstraintSet4Flag, s.ConstraintSet5Flag} {
		if f {
			cs |= 0x80 >> i
		}
	}
	return &codecInfo{Profile: name, Level: level, CodecString: fmt.Sprintf("avc1.%02X%02X%02X", s.ProfileIdc, cs, s.LevelIdc)}
}

var h265Profiles = map[uint8]string{1: "Main", 2: "Main 10", 3: "Main Still Picture", 4: "Format Range Extensions", 9: "Screen Content Coding"}

// h265Info describes a raw H.265 SPS, or returns nil when it does not parse.
func h265Info(sps []byte) *codecInfo {
	var s h265conf.SPS
	if s.Unmarshal(sps) != nil {
		return nil
	}
	p := s.ProfileTierLevel
	name, ok := h265Profiles[p.GeneralProfileIdc]
	if !ok {
		name = fmt.Sprintf("Profile %d", p.GeneralProfileIdc)
	}
	tier, tierLetter := "Main", "L"
	if p.GeneralTierFlag == 1 {
		tier, tierLetter = "High", "H"
	}
	level := fmt.Sprintf("%d.%d", p.GeneralLevelIdc/30, p.GeneralLevelIdc%30/3)

	var compat uint32
	for j, f := range p.GeneralProfileCompatibilityFlag {
		if f {
			compat |= 1 << j
		}
	}
	// The first 14 constraint bits, in stream order; the rest are zero.
	var bits uint64
	for i, f := range []bool{p.GeneralProgressiveSourceFlag, p.GeneralInterlacedSourceFlag,
		p.GeneralNonPackedConstraintFlag, p.GeneralFrameOnlyConstraintFlag, p.GeneralMax12bitConstraintFlag,
		p.GeneralMax10bitConstraintFlag, p.GeneralMax8bitConstraintFlag, p.GeneralMax422ChromeConstraintFlag,
		p.GeneralMax420ChromaConstraintFlag, p.GeneralMaxMonochromeConstraintFlag, p.GeneralIntraConstraintFlag,
		p.GeneralOnePictureOnlyConstraintFlag, p.GeneralLowerBitRateConstraintFlag, p.GeneralMax14BitConstraintFlag} {
		if f {
			bits |= 1 << (47 - i)
		}
	}
	var cons []string
	last := -1
	for i := 0; i < 6; i++ {
		if byte(bits>>(40-8*i)) != 0 {
			last = i
		}
	}
	for i := 0; i <= last; i++ {
		cons = append(cons, fmt.Sprintf("%X", byte(bits>>(40-8*i))))
	}
	space := ""
	if p.GeneralProfileSpace > 0 && p.GeneralProfileSpace < 4 {
		space = string(rune('A' + p.GeneralProfileSpace - 1))
	}
	str := fmt.Sprintf("hvc1.%s%d.%X.%s%d", space, p.GeneralProfileIdc, compat, tierLetter, p.GeneralLevelIdc)
	if len(cons) > 0 {
		str += "." + strings.Join(cons, ".")
	}
	return &codecInfo{Profile: name + " (" + tier + " tier)", Level: level, CodecString: str}
}
