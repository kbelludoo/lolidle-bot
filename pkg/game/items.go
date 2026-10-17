package game

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ItemInfo armazena metadados dos itens conhecidos do LoL Idle
type ItemInfo struct {
	ID       string
	Nome     string
	Slot     string // "botas", "arma", "armadura", "cabeca", "acessorio", "mao"
	Raridade string // "basico", "epico", "lendario"
	Lendario bool
	Preco    int
	// Afinidades de classe (0.0 a 2.0)
	AfinidadeTaric  float64
	AfinidadeLux    float64
	AfinidadeEzreal float64
	AfinidadeYasuo  float64
}

var (
	reItemFormat = regexp.MustCompile(`^([0-9]+)(?:\+(\d+))?(?:~([A-Za-z0-9?]+))?$`)
)

// ParsedItem representa uma string de item decomposta
type ParsedItem struct {
	Raw       string
	BaseID    string
	Refino    int
	Qualidade string
}

// ParseItem divide "3071+2~S" em BaseID, Refino e Qualidade
func ParseItem(x string) ParsedItem {
	m := reItemFormat.FindStringSubmatch(x)
	if len(m) == 0 {
		// Fallback se não bater regex exato
		parts := strings.Split(x, "~")
		base := parts[0]
		qual := "B"
		if len(parts) > 1 {
			qual = parts[1]
		}
		ref := 0
		if idx := strings.Index(base, "+"); idx >= 0 {
			ref, _ = strconv.Atoi(base[idx+1:])
			base = base[:idx]
		}
		return ParsedItem{Raw: x, BaseID: base, Refino: ref, Qualidade: qual}
	}

	base := m[1]
	ref := 0
	if m[2] != "" {
		ref, _ = strconv.Atoi(m[2])
	}
	qual := "B"
	if m[3] != "" {
		qual = m[3]
	}

	return ParsedItem{
		Raw:       x,
		BaseID:    base,
		Refino:    ref,
		Qualidade: qual,
	}
}

// FormatItem remonta a string padrão do jogo: "<id>[+refino][~qualidade]"
func FormatItem(base string, refino int, qual string) string {
	res := base
	if refino > 0 {
		res += fmt.Sprintf("+%d", refino)
	}
	if qual != "" {
		res += "~" + qual
	}
	return res
}

// EhBota verifica se o item é uma bota do jogo
func EhBota(id string) bool {
	base := ParseItem(id).BaseID
	switch base {
	case "1001", "3006", "3009", "3020", "3047", "3111", "3158":
		return true
	default:
		return false
	}
}

// ConheceLendario identifica os lendários conhecidos
func ConheceLendario(id string) bool {
	base := ParseItem(id).BaseID
	if info, ok := CatalogoItens[base]; ok {
		return info.Lendario
	}
	// Itens com ID >= 3000 tipicamente são épicos ou lendários no LoL
	switch base {
	case "3031", "3089", "3068", "3075", "3078", "3042", "3046", "3135", "6653", "6672",
		"3110", "3065", "3143", "3508", "3036", "3072", "3157", "4637", "3100", "3190", "3116", "3174", "3003":
		return true
	default:
		return false
	}
}

// QualidadeMult devolve o multiplicador de atributos da qualidade
func QualidadeMult(qual string) float64 {
	switch strings.ToUpper(qual) {
	case "SS":
		return 1.6
	case "S":
		return 1.3
	case "A":
		return 1.15
	case "B":
		return 1.0
	case "C":
		return 0.85
	case "?":
		return 0.9 // Pendente de sorteio no servidor
	default:
		return 1.0
	}
}

// ScoreItem calcula um score numérico do valor do item para um determinado campeão
func ScoreItem(campeao string, itemStr string) float64 {
	p := ParseItem(itemStr)
	base := p.BaseID

	var baseScore float64 = 50.0
	var afinidade float64 = 1.0

	if info, ok := CatalogoItens[base]; ok {
		baseScore = float64(info.Preco)
		if info.Lendario {
			baseScore *= 2.5
		}

		switch strings.ToLower(campeao) {
		case "taric":
			afinidade = info.AfinidadeTaric
		case "lux":
			afinidade = info.AfinidadeLux
		case "ezreal":
			afinidade = info.AfinidadeEzreal
		case "yasuo":
			afinidade = info.AfinidadeYasuo
		default:
			afinidade = 1.0
		}
	} else {
		// Heurística de ID
		idNum, _ := strconv.Atoi(base)
		if idNum >= 6000 || (idNum >= 3000 && ConheceLendario(base)) {
			baseScore = 3000.0
		} else if idNum >= 3000 {
			baseScore = 1200.0
		} else {
			baseScore = 300.0
		}
	}

	qMult := QualidadeMult(p.Qualidade)
	rMult := 1.0 + float64(p.Refino)*0.15

	return baseScore * qMult * rMult * afinidade
}

// NomeExibicaoItem devolve um nome legível para logs
func NomeExibicaoItem(itemStr string) string {
	p := ParseItem(itemStr)
	nome := p.BaseID
	if info, ok := CatalogoItens[p.BaseID]; ok {
		nome = info.Nome
	}
	suf := ""
	if p.Refino > 0 {
		suf += fmt.Sprintf("+%d", p.Refino)
	}
	if p.Qualidade != "" && p.Qualidade != "B" {
		suf += " (" + p.Qualidade + ")"
	}
	return nome + suf
}

// AutoEquip avalia a mochila e o equipamento atual, vestindo ou trocando os melhores itens.
// Retorna a lista de ações realizadas para auditoria e logs.
func AutoEquip(st *PlayerState, campeao string) []string {
	var logs []string
	if st == nil {
		return logs
	}

	if st.Itens == nil {
		st.Itens = make([]any, 0, 5)
	}

	mudou := true
	passos := 0
	for mudou && passos < 10 {
		mudou = false
		passos++

		for k := 0; k < len(st.Mochila); k++ {
			rawItem, ok := st.Mochila[k].(string)
			if !ok || rawItem == "" {
				continue
			}

			p := ParseItem(rawItem)
			novoScore := ScoreItem(campeao, rawItem)

			if EhBota(p.BaseID) {
				// Slot de botas
				botaIdx := -1
				var botaAtual string
				for j, eq := range st.Itens {
					eqStr, isStr := eq.(string)
					if isStr && EhBota(eqStr) {
						botaIdx = j
						botaAtual = eqStr
						break
					}
				}

				if botaIdx < 0 {
					// Nenhuma bota equipada: veste direto
					st.Mochila = append(st.Mochila[:k], st.Mochila[k+1:]...)
					st.Itens = append(st.Itens, rawItem)
					logs = append(logs, fmt.Sprintf("Equipou bota inicial: %s (Score: %.0f)", NomeExibicaoItem(rawItem), novoScore))
					mudou = true
					break
				} else {
					// Já tem bota: troca se a nova for estritamente melhor (+5% margem para estabilidade)
					scoreAtual := ScoreItem(campeao, botaAtual)
					if novoScore > scoreAtual*1.05 {
						st.Mochila[k] = botaAtual
						st.Itens[botaIdx] = rawItem
						logs = append(logs, fmt.Sprintf("Trocou bota: %s no lugar de %s (+%.0f score)",
							NomeExibicaoItem(rawItem), NomeExibicaoItem(botaAtual), novoScore-scoreAtual))
						mudou = true
						break
					}
				}
				continue
			}

			// Item regular (não é bota)
			// Regra 1: Não pode repetir o mesmo lendário
			if ConheceLendario(p.BaseID) {
				jaTemMesmoLendario := false
				for _, eq := range st.Itens {
					eqStr, isStr := eq.(string)
					if isStr && ParseItem(eqStr).BaseID == p.BaseID {
						jaTemMesmoLendario = true
						break
					}
				}
				if jaTemMesmoLendario {
					continue
				}
			}

			// Conta quantos itens regulares estão equipados (máx 4)
			qtdRegulares := 0
			piorScore := 1e9
			piorIdx := -1
			var piorItemStr string

			for j, eq := range st.Itens {
				eqStr, isStr := eq.(string)
				if !isStr || EhBota(eqStr) {
					continue
				}
				qtdRegulares++
				sc := ScoreItem(campeao, eqStr)
				if sc < piorScore {
					piorScore = sc
					piorIdx = j
					piorItemStr = eqStr
				}
			}

			if qtdRegulares < 4 {
				// Espaço livre nos 4 slots: equipa direto
				st.Mochila = append(st.Mochila[:k], st.Mochila[k+1:]...)
				st.Itens = append(st.Itens, rawItem)
				logs = append(logs, fmt.Sprintf("Equipou item: %s (Score: %.0f)", NomeExibicaoItem(rawItem), novoScore))
				mudou = true
				break
			} else if piorIdx >= 0 && novoScore > piorScore*1.05 {
				// Slots cheios: troca se o novo for superior ao pior equipado
				st.Mochila[k] = piorItemStr
				st.Itens[piorIdx] = rawItem
				logs = append(logs, fmt.Sprintf("Melhorou equipamento: %s no lugar de %s (+%.0f score)",
					NomeExibicaoItem(rawItem), NomeExibicaoItem(piorItemStr), novoScore-piorScore))
				mudou = true
				break
			}
		}
	}

	return logs
}

// CatalogoItens tabela com as principais peças e itens das builds ideais
var CatalogoItens = map[string]ItemInfo{
	// Botas
	"1001": {ID: "1001", Nome: "Botas da Velocidade", Slot: "botas", Raridade: "basico", Preco: 300, AfinidadeTaric: 1.0, AfinidadeLux: 1.0, AfinidadeEzreal: 1.0, AfinidadeYasuo: 1.0},
	"3006": {ID: "3006", Nome: "Grevas do Berserker", Slot: "botas", Raridade: "epico", Preco: 1100, AfinidadeTaric: 0.2, AfinidadeLux: 0.1, AfinidadeEzreal: 1.2, AfinidadeYasuo: 2.0},
	"3020": {ID: "3020", Nome: "Sapatos do Feiticeiro", Slot: "botas", Raridade: "epico", Preco: 1100, AfinidadeTaric: 0.5, AfinidadeLux: 2.0, AfinidadeEzreal: 0.8, AfinidadeYasuo: 0.1},
	"3047": {ID: "3047", Nome: "Botas Galvanizadas de Aço", Slot: "botas", Raridade: "epico", Preco: 1200, AfinidadeTaric: 2.0, AfinidadeLux: 0.3, AfinidadeEzreal: 0.9, AfinidadeYasuo: 0.8},
	"3111": {ID: "3111", Nome: "Passos de Mercúrio", Slot: "botas", Raridade: "epico", Preco: 1250, AfinidadeTaric: 1.9, AfinidadeLux: 0.5, AfinidadeEzreal: 0.9, AfinidadeYasuo: 0.9},
	"3158": {ID: "3158", Nome: "Botas Ionianas da Lucidez", Slot: "botas", Raridade: "epico", Preco: 950, AfinidadeTaric: 1.2, AfinidadeLux: 1.5, AfinidadeEzreal: 2.0, AfinidadeYasuo: 0.5},

	// Componentes Básicos
	"1029": {ID: "1029", Nome: "Couraça de Pano", Slot: "armadura", Raridade: "basico", Preco: 300, AfinidadeTaric: 1.5, AfinidadeLux: 0.3, AfinidadeEzreal: 0.4, AfinidadeYasuo: 0.5},
	"1028": {ID: "1028", Nome: "Cristal de Rubi", Slot: "armadura", Raridade: "basico", Preco: 400, AfinidadeTaric: 1.5, AfinidadeLux: 0.5, AfinidadeEzreal: 0.5, AfinidadeYasuo: 0.5},
	"1033": {ID: "1033", Nome: "Manto Anula-Magia", Slot: "armadura", Raridade: "basico", Preco: 450, AfinidadeTaric: 1.5, AfinidadeLux: 0.4, AfinidadeEzreal: 0.4, AfinidadeYasuo: 0.4},
	"1036": {ID: "1036", Nome: "Espada Longa", Slot: "arma", Raridade: "basico", Preco: 350, AfinidadeTaric: 0.2, AfinidadeLux: 0.1, AfinidadeEzreal: 1.5, AfinidadeYasuo: 1.5},
	"1052": {ID: "1052", Nome: "Tomo Amplificador", Slot: "arma", Raridade: "basico", Preco: 400, AfinidadeTaric: 0.8, AfinidadeLux: 1.8, AfinidadeEzreal: 0.8, AfinidadeYasuo: 0.1},
	"1042": {ID: "1042", Nome: "Adaga", Slot: "arma", Raridade: "basico", Preco: 250, AfinidadeTaric: 0.2, AfinidadeLux: 0.1, AfinidadeEzreal: 1.2, AfinidadeYasuo: 1.8},
	"1018": {ID: "1018", Nome: "Capa da Agilidade", Slot: "acessorio", Raridade: "basico", Preco: 600, AfinidadeTaric: 0.1, AfinidadeLux: 0.1, AfinidadeEzreal: 1.3, AfinidadeYasuo: 2.0},
	"1038": {ID: "1038", Nome: "Espada G.p.C.", Slot: "arma", Raridade: "basico", Preco: 1300, AfinidadeTaric: 0.1, AfinidadeLux: 0.1, AfinidadeEzreal: 1.8, AfinidadeYasuo: 1.9},
	"1058": {ID: "1058", Nome: "Bastão Desnecessariamente Grande", Slot: "arma", Raridade: "basico", Preco: 1250, AfinidadeTaric: 0.5, AfinidadeLux: 2.0, AfinidadeEzreal: 0.6, AfinidadeYasuo: 0.1},

	// Lendários - Taric (Tank)
	"3068": {ID: "3068", Nome: "Égide de Fogo Solar", Slot: "armadura", Raridade: "lendario", Lendario: true, Preco: 2900, AfinidadeTaric: 2.0, AfinidadeLux: 0.2, AfinidadeEzreal: 0.3, AfinidadeYasuo: 0.4},
	"3075": {ID: "3075", Nome: "Armadura de Espinhos", Slot: "armadura", Raridade: "lendario", Lendario: true, Preco: 2450, AfinidadeTaric: 2.0, AfinidadeLux: 0.2, AfinidadeEzreal: 0.2, AfinidadeYasuo: 0.3},
	"3110": {ID: "3110", Nome: "Coração Congelado", Slot: "armadura", Raridade: "lendario", Lendario: true, Preco: 2400, AfinidadeTaric: 2.0, AfinidadeLux: 0.3, AfinidadeEzreal: 0.4, AfinidadeYasuo: 0.2},
	"3065": {ID: "3065", Nome: "Semblante Espiritual", Slot: "armadura", Raridade: "lendario", Lendario: true, Preco: 2900, AfinidadeTaric: 1.9, AfinidadeLux: 0.3, AfinidadeEzreal: 0.3, AfinidadeYasuo: 0.3},
	"3143": {ID: "3143", Nome: "Presságio de Randuin", Slot: "armadura", Raridade: "lendario", Lendario: true, Preco: 2700, AfinidadeTaric: 1.8, AfinidadeLux: 0.2, AfinidadeEzreal: 0.2, AfinidadeYasuo: 0.3},

	// Lendários - Lux (AP Burst)
	"3089": {ID: "3089", Nome: "Capuz da Morte de Rabadon", Slot: "cabeca", Raridade: "lendario", Lendario: true, Preco: 3500, AfinidadeTaric: 0.5, AfinidadeLux: 2.0, AfinidadeEzreal: 0.8, AfinidadeYasuo: 0.1},
	"6653": {ID: "6653", Nome: "Tormento de Liandry", Slot: "cabeca", Raridade: "lendario", Lendario: true, Preco: 3000, AfinidadeTaric: 0.7, AfinidadeLux: 1.9, AfinidadeEzreal: 0.7, AfinidadeYasuo: 0.1},
	"3135": {ID: "3135", Nome: "Cajado do Vazio", Slot: "arma", Raridade: "lendario", Lendario: true, Preco: 3000, AfinidadeTaric: 0.4, AfinidadeLux: 2.0, AfinidadeEzreal: 0.6, AfinidadeYasuo: 0.1},
	"3157": {ID: "3157", Nome: "Ampulheta de Zhonya", Slot: "acessorio", Raridade: "lendario", Lendario: true, Preco: 3250, AfinidadeTaric: 0.8, AfinidadeLux: 1.8, AfinidadeEzreal: 0.8, AfinidadeYasuo: 0.2},
	"4637": {ID: "4637", Nome: "Foco do Horizonte", Slot: "acessorio", Raridade: "lendario", Lendario: true, Preco: 2700, AfinidadeTaric: 0.3, AfinidadeLux: 1.9, AfinidadeEzreal: 0.7, AfinidadeYasuo: 0.1},

	// Lendários - Ezreal (ADC Híbrido)
	"3078": {ID: "3078", Nome: "Força da Trindade", Slot: "arma", Raridade: "lendario", Lendario: true, Preco: 3333, AfinidadeTaric: 0.4, AfinidadeLux: 0.3, AfinidadeEzreal: 2.0, AfinidadeYasuo: 1.3},
	"3042": {ID: "3042", Nome: "Muramana", Slot: "arma", Raridade: "lendario", Lendario: true, Preco: 2900, AfinidadeTaric: 0.3, AfinidadeLux: 0.4, AfinidadeEzreal: 2.0, AfinidadeYasuo: 0.7},
	"3508": {ID: "3508", Nome: "Colhedor de Essência", Slot: "arma", Raridade: "lendario", Lendario: true, Preco: 3100, AfinidadeTaric: 0.2, AfinidadeLux: 0.2, AfinidadeEzreal: 1.9, AfinidadeYasuo: 1.4},
	"3036": {ID: "3036", Nome: "Lorde Dominik", Slot: "arma", Raridade: "lendario", Lendario: true, Preco: 3000, AfinidadeTaric: 0.2, AfinidadeLux: 0.2, AfinidadeEzreal: 1.8, AfinidadeYasuo: 1.8},

	// Lendários - Yasuo (Melee Crit/AS)
	"6672": {ID: "6672", Nome: "Mata-Cráquens", Slot: "arma", Raridade: "lendario", Lendario: true, Preco: 3100, AfinidadeTaric: 0.2, AfinidadeLux: 0.1, AfinidadeEzreal: 1.4, AfinidadeYasuo: 2.0},
	"3031": {ID: "3031", Nome: "Gume do Infinito", Slot: "arma", Raridade: "lendario", Lendario: true, Preco: 3500, AfinidadeTaric: 0.2, AfinidadeLux: 0.1, AfinidadeEzreal: 1.8, AfinidadeYasuo: 2.0},
	"3046": {ID: "3046", Nome: "Dançarina Fantasma", Slot: "acessorio", Raridade: "lendario", Lendario: true, Preco: 2650, AfinidadeTaric: 0.2, AfinidadeLux: 0.1, AfinidadeEzreal: 1.3, AfinidadeYasuo: 1.9},
	"3072": {ID: "3072", Nome: "A Sedenta por Sangue", Slot: "arma", Raridade: "lendario", Lendario: true, Preco: 3400, AfinidadeTaric: 0.3, AfinidadeLux: 0.1, AfinidadeEzreal: 1.6, AfinidadeYasuo: 1.8},
}
