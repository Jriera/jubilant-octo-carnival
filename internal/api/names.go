package api

// Names of currencies the ECB quotes now or has quoted since 1999.
var Names = map[string]string{
	"AUD": "Australian dollar", "BGN": "Bulgarian lev", "BRL": "Brazilian real",
	"CAD": "Canadian dollar", "CHF": "Swiss franc", "CNY": "Chinese yuan renminbi",
	"CYP": "Cypriot pound", "CZK": "Czech koruna", "DKK": "Danish krone",
	"EEK": "Estonian kroon", "EUR": "Euro", "GBP": "Pound sterling",
	"HKD": "Hong Kong dollar", "HRK": "Croatian kuna", "HUF": "Hungarian forint",
	"IDR": "Indonesian rupiah", "ILS": "Israeli shekel", "INR": "Indian rupee",
	"ISK": "Icelandic krona", "JPY": "Japanese yen", "KRW": "South Korean won",
	"LTL": "Lithuanian litas", "LVL": "Latvian lats", "MTL": "Maltese lira",
	"MXN": "Mexican peso", "MYR": "Malaysian ringgit", "NOK": "Norwegian krone",
	"NZD": "New Zealand dollar", "PHP": "Philippine peso", "PLN": "Polish zloty",
	"ROL": "Romanian leu (old)", "RON": "Romanian leu", "RUB": "Russian rouble",
	"SEK": "Swedish krona", "SGD": "Singapore dollar", "SIT": "Slovenian tolar",
	"SKK": "Slovak koruna", "THB": "Thai baht", "TRL": "Turkish lira (old)",
	"TRY": "Turkish lira", "USD": "US dollar", "ZAR": "South African rand",
}

// Names of further currencies quoted by NBP tables A and B.
func init() {
	for k, v := range map[string]string{
		"AED": "UAE dirham", "AFN": "Afghan afghani", "ALL": "Albanian lek", "AMD": "Armenian dram",
		"ANG": "Netherlands Antillean guilder", "AOA": "Angolan kwanza", "ARS": "Argentine peso",
		"AWG": "Aruban florin", "AZN": "Azerbaijani manat", "BAM": "Bosnia and Herzegovina convertible mark",
		"BBD": "Barbados dollar", "BDT": "Bangladeshi taka", "BHD": "Bahraini dinar", "BIF": "Burundian franc",
		"BND": "Brunei dollar", "BOB": "Bolivian boliviano", "BSD": "Bahamian dollar", "BWP": "Botswana pula",
		"BYN": "Belarusian rouble", "BZD": "Belize dollar", "CDF": "Congolese franc", "CLP": "Chilean peso",
		"COP": "Colombian peso", "CRC": "Costa Rican colon", "CUP": "Cuban peso", "CVE": "Cape Verdean escudo",
		"DJF": "Djiboutian franc", "DOP": "Dominican peso", "DZD": "Algerian dinar", "EGP": "Egyptian pound",
		"ERN": "Eritrean nakfa", "ETB": "Ethiopian birr", "FJD": "Fiji dollar", "GEL": "Georgian lari",
		"GHS": "Ghanaian cedi", "GIP": "Gibraltar pound", "GMD": "Gambian dalasi", "GNF": "Guinean franc",
		"GTQ": "Guatemalan quetzal", "GYD": "Guyanese dollar", "HNL": "Honduran lempira", "HTG": "Haitian gourde",
		"IQD": "Iraqi dinar", "IRR": "Iranian rial", "JMD": "Jamaican dollar", "JOD": "Jordanian dinar",
		"KES": "Kenyan shilling", "KGS": "Kyrgyzstani som", "KHR": "Cambodian riel", "KMF": "Comorian franc",
		"KWD": "Kuwaiti dinar", "KZT": "Kazakhstani tenge", "LAK": "Lao kip", "LBP": "Lebanese pound",
		"LKR": "Sri Lankan rupee", "LRD": "Liberian dollar", "LSL": "Lesotho loti", "LYD": "Libyan dinar",
		"MAD": "Moroccan dirham", "MDL": "Moldovan leu", "MGA": "Malagasy ariary", "MKD": "Macedonian denar",
		"MMK": "Myanmar kyat", "MNT": "Mongolian tugrik", "MOP": "Macanese pataca", "MRU": "Mauritanian ouguiya",
		"MUR": "Mauritian rupee", "MVR": "Maldivian rufiyaa", "MWK": "Malawian kwacha", "MZN": "Mozambican metical",
		"NAD": "Namibian dollar", "NGN": "Nigerian naira", "NIO": "Nicaraguan cordoba", "NPR": "Nepalese rupee",
		"OMR": "Omani rial", "PAB": "Panamanian balboa", "PEN": "Peruvian sol", "PGK": "Papua New Guinean kina",
		"PKR": "Pakistani rupee", "PYG": "Paraguayan guarani", "QAR": "Qatari riyal", "RSD": "Serbian dinar",
		"RWF": "Rwandan franc", "SAR": "Saudi riyal", "SBD": "Solomon Islands dollar", "SCR": "Seychellois rupee",
		"SDG": "Sudanese pound", "SLE": "Sierra Leonean leone", "SOS": "Somali shilling", "SRD": "Surinamese dollar",
		"SSP": "South Sudanese pound", "STN": "Sao Tome and Principe dobra", "SVC": "Salvadoran colon",
		"SYP": "Syrian pound", "SZL": "Eswatini lilangeni", "TJS": "Tajikistani somoni", "TMT": "Turkmenistan manat",
		"TND": "Tunisian dinar", "TOP": "Tongan pa'anga", "TTD": "Trinidad and Tobago dollar", "TWD": "New Taiwan dollar",
		"TZS": "Tanzanian shilling", "UAH": "Ukrainian hryvnia", "UGX": "Ugandan shilling", "UYU": "Uruguayan peso",
		"UZS": "Uzbekistani sum", "VES": "Venezuelan bolivar", "VND": "Vietnamese dong", "VUV": "Vanuatu vatu",
		"WST": "Samoan tala", "XAF": "Central African CFA franc", "XCD": "East Caribbean dollar",
		"XDR": "IMF special drawing right", "XOF": "West African CFA franc", "XPF": "CFP franc",
		"YER": "Yemeni rial", "ZMW": "Zambian kwacha", "ZWG": "Zimbabwe gold", "ZWL": "Zimbabwean dollar",
	} {
		if _, ok := Names[k]; !ok {
			Names[k] = v
		}
	}
}
