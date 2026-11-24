package layout

// Column is a vertical list of equally spaced click targets.
type Column struct {
	X int `json:"x"`
	// FirstY is the vertical position of the first target.
	FirstY int `json:"firstY"`
	// StepY is the distance between targets.
	StepY int `json:"stepY"`
}

// At returns the position of target i, counting from zero.
func (c Column) At(i int) Point {
	return Point{X: c.X, Y: c.FirstY + c.StepY*i}
}

// Characters locates the character screens: the attributes page, and the
// constellation and talent pages that open over it.
type Characters struct {
	// Header holds the element and name, as in "Pyro / Hu Tao".
	Header Box `json:"header"`
	// Level holds the level and its cap, as in "Lv. 80/90".
	Level Box `json:"level"`
	// NextCharacter is the arrow that moves to the next character.
	NextCharacter Point `json:"nextCharacter"`
	// The three tabs on the left of the screen.
	AttributesTab    Point `json:"attributesTab"`
	ConstellationTab Point `json:"constellationTab"`
	TalentsTab       Point `json:"talentsTab"`
	// Constellations lists the six constellation nodes.
	Constellations Column `json:"constellations"`
	// Activate samples beside the "Activate" label, which is white for a
	// constellation that isn't unlocked yet.
	Activate Box `json:"activate"`
	// Talents lists the talents. Characters with an alternate sprint have it before
	// the burst, so the burst is one step further down for them.
	Talents Column `json:"talents"`
	// TalentLevel holds the level of the selected talent.
	TalentLevel Box `json:"talentLevel"`
}
