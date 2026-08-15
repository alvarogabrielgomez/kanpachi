// Package layout holds the names Kanpachi writes on disk that MORE THAN ONE
// binary has to agree on.
//
// # Why it exists
//
// Because the two that need them cannot import each other. The daemon is
// `package main`, so nothing can reach into it, and the portable launcher in
// `internal/kanpachibundle` needs the same names to find the log it exists to
// leave behind and to point the daemon at its data folder. What was there
// before was a copy in each, with a comment in one saying it had to keep saying
// what the other said. Renaming one renamed one.
//
// # What goes here, and what does not
//
// Only names that two binaries have to spell identically or something breaks
// quietly. Not paths, which are built from these plus wherever the machine puts
// its data, and not anything the two sides of a ROOM have to derive the same
// way: that lives in `core/domain`, frozen, with golden vectors, for the reason
// `internal/brand` spells out.
package layout

const (
	// LogDir is the folder the daemon's log lives in, inside the data
	// directory.
	LogDir = "logs"

	// LogFile is the log itself. It is the file somebody is asked to send when
	// something went wrong, so its name shows up in documentation and in the
	// window: it changes here or it does not change.
	LogFile = "kanpachi.log"

	// PortableDataDir is the state folder of a portable Kanpachi, NEXT TO the
	// executable somebody double-clicked.
	//
	// **It is not called `data`, and that is measured.** Flutter's Windows
	// bundle ships its own `data\` with `icudtl.dat`, `app.so` and
	// `flutter_assets\`, and in a portable folder both executables live in the
	// same directory: they mixed. The daemon would have written its token and
	// its key inside the window's resources, and clearing the data would have
	// taken the assets with it. That came out of running the script, not out of
	// reading the code.
	PortableDataDir = "kanpachi-data"
)
