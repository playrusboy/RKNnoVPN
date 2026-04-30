package main

import (
	"log"

	"github.com/youtubediscord/RKNnoVPN/daemon/internal/runtimev2"
)

func (d *daemon) installRescueCallbacks() {
	d.healthMon.SetOnDegraded(func() {
		epoch := d.currentRuntimeOperationEpoch()
		if !d.canRunRuntimeRecovery(epoch) {
			log.Printf("rescue skipped: runtime is no longer desired running")
			return
		}
		_, err := d.runtimeV2.RunOperation(runtimev2.OperationRescue, runtimev2.PhaseStarting, func(generation int64) error {
			if !d.canRunRuntimeRecovery(epoch) {
				log.Printf("rescue skipped: runtime changed before recovery")
				return nil
			}
			d.mu.Lock()
			rescueEnabled := d.cfg.Rescue.Enabled
			d.mu.Unlock()
			if !rescueEnabled {
				log.Printf("rescue disabled, skipping automatic recovery")
				return nil
			}
			if err := d.rescueMgr.Attempt(func() bool {
				return d.canRunRuntimeRecovery(epoch)
			}); err != nil {
				log.Printf("rescue attempt failed: %v", err)
				d.mu.Lock()
				maxAttempts := d.cfg.Rescue.MaxAttempts
				d.mu.Unlock()
				if maxAttempts < 1 {
					maxAttempts = 1
				}
				if d.rescueMgr.Attempts() >= maxAttempts && d.canRunRuntimeRecovery(epoch) {
					if rollbackErr := d.rescueMgr.Rollback(); rollbackErr != nil {
						log.Printf("rescue rollback failed: %v", rollbackErr)
					}
				}
				return nil
			}
			return nil
		})
		if err != nil {
			log.Printf("rescue skipped or failed: %v", err)
		}
	})
	d.healthMon.SetOnRestored(func() {
		d.rescueMgr.Reset()
	})
}
