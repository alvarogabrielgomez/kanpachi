//go:build !windows && !linux

package sysevents

// subscribe fuera de Windows no abre nada, y los tres canales quedan mudos.
//
// Es exactamente lo que el provisional hacía, y sigue siendo la verdad: en esta
// plataforma no hay identificación de red de Windows, ni reanudación que
// invalide un adaptador virtual que tampoco existe. Un canal mudo no afirma
// nada falso.
func (e *Events) subscribe() {
	e.log.Info("los avisos del sistema solo están escritos para Windows")
}
