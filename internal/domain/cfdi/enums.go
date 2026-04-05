package cfdi

// FormaPago SAT payment form codes.
var FormaPagoBancarizadas = map[string]bool{
	"02": true, // Cheque nominativo
	"03": true, // Transferencia electrónica de fondos
	"04": true, // Tarjeta de crédito
	"05": true, // Monedero electrónico
	"06": true, // Dinero electrónico
	"28": true, // Tarjeta de débito
	"29": true, // Tarjeta de servicios
}

func FormaPagoBancarizadasList() []string {
	return []string{"02", "03", "04", "05", "06", "28", "29"}
}

func FormaPagoNoBancarizadasList() []string {
	return []string{"01", "08", "12", "13", "14", "15", "17", "23", "24", "25", "26", "27", "30", "31", "99"}
}

// UsoCFDI SAT usage codes.
var UsoCFDIBancarizadas = map[string]bool{
	"G01": true, // Adquisición de mercancías
	"G03": true, // Gastos en general
}

func UsoCFDIBancarizadasList() []string {
	return []string{"G01", "G03"}
}

var UsoCFDIInversiones = []string{
	"I01", // Construcciones
	"I02", // Mobiliario y equipo de oficina por inversiones
	"I03", // Equipo de transporte
	"I04", // Equipo de cómputo y accesorios
	"I05", // Dados, troqueles, moldes, matrices y herramental
	"I06", // Comunicaciones telefónicas
	"I07", // Comunicaciones satelitales
	"I08", // Otra maquinaria y equipo
}
