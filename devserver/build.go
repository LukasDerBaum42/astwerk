package devserver

import "github.com/LukasDerBaum42/astwerk/ssg"

// Command is [ssg.Command], re-exported here so the devserver API stays as it
// was. The build-step helpers live with the build system in [ssg]; this
// package forwards for callers who reach for them here.
//
//	Build: devserver.Command("go", "run", ".", "--out", "build")
func Command(name string, args ...string) func() error { return ssg.Command(name, args...) }

// Steps is [ssg.Steps], re-exported here for the same reason as [Command].
//
//	Build: devserver.Steps(
//		devserver.Command("templ", "generate"),
//		devserver.Command("go", "run", ".", "--out", "build"),
//	)
func Steps(steps ...func() error) func() error { return ssg.Steps(steps...) }
