package layout

// Weapons locates the weapon inventory screen. Its detail panel sits where the
// artifact one does, but holds a level and a refinement instead of stats.
type Weapons struct {
	Grid      Grid `json:"grid"`
	ItemCount Box  `json:"itemCount"`
	Panel     Box  `json:"panel"`
	Name      Box  `json:"name"`
	// Nameplate is the coloured banner behind the name, whose colour gives the rarity.
	Nameplate  Box `json:"nameplate"`
	Level      Box `json:"level"`
	Refinement Box `json:"refinement"`
	Equipped   Box `json:"equipped"`
	// EquippedBanner samples the strip the "Equipped" line is drawn on, which only
	// exists for a weapon a character is holding.
	EquippedBanner Box `json:"equippedBanner"`
	// LockIcon samples a corner of the lock toggle, which is dark when on.
	LockIcon Box `json:"lockIcon"`
}

// Browse returns what the walker needs from the weapon screen.
func (w Weapons) Browse() Browse {
	return Browse{Grid: w.Grid, ItemCount: w.ItemCount, Panel: w.Panel}
}
