package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestCommandSkipsStoreInit_AdminResetOnly(t *testing.T) {
	root := &cobra.Command{Use: "bd"}
	admin := &cobra.Command{Use: "admin"}
	reset := &cobra.Command{Use: "reset"}
	cleanup := &cobra.Command{Use: "cleanup"}
	compact := &cobra.Command{Use: "compact"}
	root.AddCommand(admin)
	admin.AddCommand(reset, cleanup, compact)

	if !commandSkipsStoreInit(reset) {
		t.Fatal("bd admin reset should skip store initialization so it can recover broken local state")
	}
	if commandSkipsStoreInit(cleanup) {
		t.Fatal("bd admin cleanup should still initialize the store")
	}
	if commandSkipsStoreInit(compact) {
		t.Fatal("bd admin compact should still initialize the store")
	}
}

func TestCommandSkipsStoreInit_DoltStoreExceptions(t *testing.T) {
	root := &cobra.Command{Use: "bd"}
	dolt := &cobra.Command{Use: "dolt"}
	show := &cobra.Command{Use: "show"}
	push := &cobra.Command{Use: "push"}
	remote := &cobra.Command{Use: "remote"}
	remoteAdd := &cobra.Command{Use: "add"}
	root.AddCommand(dolt)
	dolt.AddCommand(show, push, remote)
	remote.AddCommand(remoteAdd)

	if !commandSkipsStoreInit(show) {
		t.Fatal("bd dolt show should remain a no-store diagnostic command")
	}
	if commandSkipsStoreInit(push) {
		t.Fatal("bd dolt push should still initialize the store")
	}
	if commandSkipsStoreInit(remoteAdd) {
		t.Fatal("bd dolt remote add should still initialize the store")
	}
}
