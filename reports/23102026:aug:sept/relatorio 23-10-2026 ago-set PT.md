# Relatório de Viabilidade Fotovoltaica: Alhandra / São João dos Montes (dados de agosto–setembro de 2026)

**Data:** 23/10/2026 (atualizado com as coordenadas exatas do telhado)
**Dados:** exportação de energia Shelly, 1 de agosto – 30 de setembro de 2026 (61 dias, dois meses de verão)
**Localização:** telhado em 38,950780 N, −9,037919 E (São João dos Montes, Vila Franca de Xira), cerca de 125 m de altitude
**Substitui:** o relatório de 23/10/2026 baseado apenas em setembro

> **Como ler este relatório.** Os valores de agosto e setembro são **medidos**. Os valores anuais são **cenários modelados**, extrapolados a partir de dois meses de consumo de verão. Os custos e preços de venda de excedente são **indicações de mercado, não orçamentos**.

---

## 1. Recomendação

**Instalar cerca de 4 kWp, repartidos entre as águas do telhado viradas a sul e a oeste, sem bateria para já, e pôr o carro elétrico a carregar com a energia solar.**

- **Retorno do investimento:** cerca de **12 anos** a preços médios de instalação de 2026, com uma poupança de cerca de **545 €/ano**. Se pelo menos metade do carregamento diário do carro passar para as horas de sol, o retorno desce para cerca de **9,5 anos** e a poupança sobe para cerca de 685 €/ano.
- **Se comprar o equipamento e um técnico responsável o ligar e registar** (cerca de 2 400–3 400 € em vez de 6 500 €), o retorno seria de cerca de **4,5–6 anos**.
- **Porque não 3 kWp:** o retorno é ligeiramente mais curto (11,4 anos), mas poupa menos no total. Por si só, o 4.º kWp paga-se em cerca de 14,5 anos, dentro da vida útil de 25 anos dos painéis.
- **Porque não 5 kWp ou mais:** cada kWp adicional acrescenta sobretudo excedente mal pago. Por si só, o 5.º kWp paga-se em cerca de 17 anos, o 6.º em cerca de 19 e o 7.º–8.º em cerca de 22. Só vale a pena considerar 5 kWp se o carro estiver habitualmente em casa e a carregar durante o dia.
- **Manter o inversor em 4 kW ou menos.** Acima de 4 kW, as regras portuguesas obrigam à contagem de toda a produção, a cargo do proprietário. Com os painéis repartidos entre sul e oeste, um inversor de 4 kW quase não perde produção, mesmo com 5 kWp de painéis.
- **As baterias não se justificam aos preços atuais.** O custo adicional paga-se em cerca de 17 anos (5 kWh) e 19 anos (10 kWh), mais do que a garantia típica de 10 anos de uma bateria.
- **Se o espaço no telhado for limitado, dar prioridade à água virada a sul.** Em dezembro produz cerca de 65% mais do que a água virada a oeste (ver secção 3).

**O que mudou com agosto e com a localização exata:**
- Ao juntar agosto, o consumo anual estimado desceu de cerca de 9 230 para cerca de **8 725 kWh/ano**, o que reduz a poupança em cerca de 30 €/ano.
- As coordenadas exatas confirmam que **o relevo à volta não faz sombra ao telhado** (custa menos de 0,2% da produção) e aumentam ligeiramente a produção estimada.
- **A recomendação mantém-se.** Usando só agosto ou só setembro como base de consumo, o retorno de 4 kWp fica num intervalo estreito (11,2–12,5 anos).

---

## 2. O que os dados mostram (medido)

### A exportação

- Abrange 1 de agosto – 30 de setembro de 2026, hora de Lisboa (UTC+1 durante todo o período, sem mudança de hora).
- O contador geral é lido a cada 10 segundos; os ficheiros têm totais por 15 minutos, por hora, por dia e por mês.
- O consumo da casa é igual à energia comprada à rede, porque não há produção e não foi registada injeção na rede.
- As linhas de setembro são idênticas às da exportação anterior, só de setembro.

### Consumo

| Indicador | Agosto | Setembro | Os dois meses |
|---|---|---|---|
| Consumo total | **701,5 kWh** | **756,5 kWh** | **1 458,0 kWh** |
| Custo variável (cálculo da app) | 112,74 € | 124,10 € | 236,84 € |
| Média diária (todos os dias) | 22,6 kWh | 25,2 kWh | 23,9 kWh |
| Intervalo diário (desvio-padrão) | 6,5–47,2 kWh (9,0) | 17,6–37,0 kWh (4,9) | 6,5–47,2 kWh (7,4) |
| Dias úteis vs fim de semana, só dias completos | 23,3 vs 22,4 kWh | 25,2 vs 23,3 kWh | Sem diferença relevante |
| Parte diurna (08:00–20:00) | 50% | 49% | 50% |
| Consumo de base noturno, sem o carro (03:00–07:00) | cerca de 295 W | cerca de 375 W | cerca de 335 W |
| Pico de potência (leitura de 10 segundos) | 8,81 kW (5 ago) | 8,87 kW (25 set) | 8,87 kW |
| Média mais alta em 15 minutos | 5,76 kW | 5,42 kW | 5,76 kW |
| Carregamento do carro elétrico | 296,7 kWh (42%), 43 sessões | 275,1 kWh (36%), 48 sessões | **571,8 kWh (39%)** |
| Carregamento do carro durante o dia | 40% | 34% | 37% |

### Dias de consumo muito baixo (provavelmente férias ou ausências)

- 7–8 de agosto e 13–15 de agosto tiveram apenas 6,5–15 kWh/dia, quase sem carregamento do carro. O dia 15 de agosto é feriado.
- São comportamento real e foram mantidos na análise, o que corresponde aproximadamente às férias de um ano normal.

### Perfil diário de consumo (potência média em watts, por hora)

| Hora | 0 | 1 | 2 | 3–8 | 9 | 10 | 11 | 12–16 | 17–20 | 21 | 22 | 23 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Agosto | 1 754 | 1 226 | 673 | 285–434 | 380 | 467 | 474 | 955–1 235 | 1 154–1 374 | 1 099 | 1 927 | 1 897 |
| Setembro | 1 749 | 1 013 | 560 | 356–393 | 947 | 1 168 | 973 | 960–1 266 | 1 101–1 293 | 1 592 | 2 431 | 2 263 |

- Madrugada (03:00–08:00): cerca de 290–390 W.
- **As manhãs de agosto (09:00–11:00) têm um consumo muito mais baixo do que as de setembro** (cerca de 400 W contra cerca de 1 kW). O ar condicionado do escritório também foi muito menos usado em agosto (13,5 vs 55 kWh), o que sugere menos teletrabalho.
- Pico ao fim do dia (22:00–01:00): cerca de 1,7–2,4 kW, sobretudo carregamento do carro.
- Sem contar com o carro, 57–58% do consumo já acontece nas horas de sol, nos dois meses.

### Consumo por equipamento

As leituras por equipamento são uma decomposição do total da casa e não são somadas a ele, por isso nada é contado duas vezes.

| Equipamento | Agosto kWh | Setembro kWh | Parte diurna (ago / set) |
|---|---|---|---|
| Carregador do carro | 296,7 | 275,1 | 40% / 36% |
| Servidor/rede do escritório (constante, ~90–100 W) | 65,1 | 72,2 | 50% / 50% |
| Tomadas da cozinha (provavelmente o frigorífico) | 61,8 | 68,8 | 55% / 54% |
| Garagem "Tests" (constante, ~59 W, não identificado) | 43,8 | 42,4 | 50% / 49% |
| Forno/placa | 32,2 | 28,2 | 68% / 59% |
| Máquina de lavar da cozinha | 24,7 | 25,8 | 48% / 39% |
| Máquinas da lavandaria | 17,1 | 23,6 | 68% / 83% |
| Ar condicionado do escritório | 13,5 | 55,0 | 79% / 79% |
| Iluminação, toalheiro, lareira | 3,0 | 1,0 | n/d |
| Não abrangido por nenhum equipamento | cerca de 143,7 (20%) | cerca de 164,5 (22%) | n/d |

### Qualidade dos dados

- Sem registos duplicados.
- **Agosto tem dados muito mais completos:** 27 dos 31 dias têm pelo menos 90% dos dados, contra 16 dos 30 em setembro.
- 14 falhas de recolha, num total de cerca de 52 horas (3,6% do período). Apenas 3 horas foram em agosto; as maiores foram de 15 h a 9–10 de setembro e de cerca de 9 h a 6 e a 30 de setembro.
- Depois de cada falha, o contador registou a energia em falta no intervalo seguinte, até 19,6 kWh num único intervalo de 15 minutos. Os totais estão certos, mas não se sabe como o consumo se distribuiu dentro das falhas.
  - Cerca de 69 kWh (4,7% do período) foram redistribuídos pelas falhas segundo o perfil diário típico. **Essa distribuição é modelada, não medida.**
- Em setembro houve 4 intervalos com reinício do contador, pelo que uma pequena quantidade de energia não é recuperável.
- Não existe histórico de energia injetada na rede.
- **Aviso de retenção:** os dados de 15 minutos só são guardados durante 90 dias, por isso os dados de 15 minutos do início de agosto começam a desaparecer por volta do fim de outubro de 2026. Esta exportação já os preserva.

---

## 3. O local: percurso do sol e relevo (modelado a partir das coordenadas exatas)

### O relevo à volta da casa

- O modelo de terreno do PVGIS dá um horizonte de apenas **0–7°** em todas as direções.
  - Mais baixo a sudeste (cerca de 0–1,5° entre 120° e 150°), que é por onde o sol nasce no inverno.
  - Mais alto a sul-sudoeste e sudoeste (cerca de 6–7° entre 195° e 225°) e a norte-nordeste.
- Comparando o PVGIS com e sem este horizonte, **o relevo custa cerca de 0,04% da produção anual na água sul e 0,2% na água oeste.** O sombreamento do terreno não é um problema.
- **O modelo de terreno não inclui prédios, árvores nem chaminés.** Os obstáculos próximos continuam a ter de ser verificados no local.

### O sol ao longo do ano

| | 21 dez (inverno) | 20 mar / 22 set (equinócios) | 21 jun (verão) |
|---|---|---|---|
| Nascer / pôr do sol | 07:52 / 17:17 | 06:40 / 18:48 (mar); 07:24 / 19:33 (set) | 06:11 / 21:05 |
| Direção do nascer / pôr do sol | 120° (ESE) / 240° (OSO) | 90° (E) / 271° (O) | 58° (ENE) / 302° (ONO) |
| Horas de luz | 9,4 h | 12,1 h | 14,9 h |
| Altura máxima do sol | **27,6°** às 12:34 | 51° às 12:44 (mar) / 13:29 (set) | **74,5°** às 13:38 |
| Sol acima do horizonte do relevo | 08:05–16:35 (8,5 h) | 07:02–18:25 / 07:46–19:11 (11,4 h) | 06:52–20:29 (13,6 h) |
| Sol direto na água **sul** | 8,5 h (08:05–16:35) | 11,4 h | 12,8 h (07:14–20:02) |
| Sol direto na água **oeste** | 6,8 h (a partir das 09:48) | 9,6–9,7 h (a partir das 08:48 / 09:33) | 12,0 h (a partir das 08:32) |

### Produção num dia típico, 4 kWp (2 kWp a sul + 2 kWp a oeste, média de 2014–2023)

| Mês | Água sul | Água oeste | Total por dia | Dia nublado (percentil 10) | Dia de sol (percentil 90) | Melhor hora |
|---|---|---|---|---|---|---|
| Dezembro | 5,1 kWh | 3,1 kWh | **8,2 kWh** | 3,9 kWh | 11,7 kWh | 13:00, cerca de 1,4 kWh |
| Março | 8,3 kWh | 6,7 kWh | **14,9 kWh** | 7,2 kWh | 19,8 kWh | 13:00, cerca de 2,1 kWh |
| Junho | 10,3 kWh | 10,4 kWh | **20,7 kWh** | 13,9 kWh | 24,6 kWh | 14:00, cerca de 2,4 kWh |
| Setembro | 9,3 kWh | 7,8 kWh | **17,1 kWh** | 12,5 kWh | 20,2 kWh | 14:00, cerca de 2,3 kWh |

### O que isto significa para o projeto

- **No inverno é a água sul que faz a maior parte do trabalho.** Em dezembro o sol sobe apenas até 27,6°, e a água oeste não recebe sol direto antes das 09:50, por isso produz cerca de 60% do que produz a água sul. Em junho as duas produzem praticamente o mesmo.
- **A água oeste desloca a produção para o fim da tarde.** Em junho ainda produz cerca de 0,7 kWh entre as 19:00 e as 20:00, contra cerca de 0,2 kWh da água sul. Isso coincide com o seu consumo das 17:00 às 20:00, por isso continua a fazer sentido repartir os painéis.
- **No inverno quase toda a produção seria consumida em casa.** Um dia típico de dezembro produz cerca de 8 kWh, para uma casa que gasta cerca de 24 kWh/dia. O problema do excedente (e o interesse em deslocar o carregamento do carro ou ter bateria) é sobretudo da primavera e do verão.
- **Regra prática para obstáculos próximos:** ao meio-dia em dezembro (sol a 27,6°), um obstáculo a sul faz uma sombra com cerca de **2 vezes a sua altura**; às 10:00 (sol a cerca de 18°), cerca de **3 vezes a sua altura**. Por exemplo, uma árvore ou um prédio de 5 m a menos de 10–15 m a sul ou sudeste dos painéis faria sombra nas manhãs de inverno.
- **A inclinação do telhado pouco importa.** A inclinação ótima para um telhado virado a sul neste local é 33°, que produz só cerca de 1% mais do que os 25° assumidos.

---

## 4. Comparação de sistemas fotovoltaicos (modelado)

### Estimativa de produção (PVGIS)

- Fonte: PVGIS nas coordenadas exatas do telhado, montagem em telhado, dados meteorológicos horários de 2014–2023, com a perda de sistema padrão de 14% do PVGIS (inversor, cablagem e sujidade dos painéis), mais perdas por temperatura e o horizonte do relevo.
- Inclinação assumida de 25°, com as águas viradas exatamente a sul (180°) e a oeste (270°). **São pressupostos; meça a inclinação e a orientação reais.**
- Produção anual por kWp: sul 1 507 kWh, oeste 1 283 kWh. A variação de ano para ano é de cerca de ±3–4%.
- Cenário base com 50% dos painéis a sul e 50% a oeste.

### Método

- Cada ano (meteorologia de 2014–2023) é simulado cronologicamente em passos de 15 minutos.
- O consumo da casa repete os 61 dias de agosto e setembro já limpos, respeitando dias úteis e fins de semana.
- O excedente injetado é valorizado a 0,03 €/kWh.

### Resultados

| | 3 kWp | **4 kWp** | 5 kWp | 6 kWp | 8 kWp |
|---|---|---|---|---|---|
| Produção anual, kWh (intervalo entre anos) | 4 161 (3 971–4 353) | **5 547** (5 294–5 803) | 6 934 | 8 321 | 11 095 |
| Produção em janeiro / julho, kWh | 211 / 503 | 281 / 671 | 351 / 839 | 422 / 1 007 | 562 / 1 342 |
| Energia solar consumida diretamente, kWh | 2 387 | **2 705** | 2 942 | 3 117 | 3 342 |
| Parte da produção consumida em casa | 57% | 49% | 42% | 37% | 30% |
| Parte do consumo da casa coberta pelo solar | 27% | 31% | 34% | 36% | 38% |
| Energia comprada à rede, kWh (a partir de cerca de 8 725) | 6 338 | 6 020 | 5 783 | 5 608 | 5 384 |
| Excedente injetado, kWh | 1 774 | 2 843 | 3 992 | 5 204 | 7 753 |
| **Com 50% do carregamento do carro nas horas de sol:** parte do consumo coberta pelo solar | 36% | 42% | 47% | 50% | 53% |

### Produção mensal, kWh (média de 2014–2023, 50/50 sul/oeste)

| kWp | Jan | Fev | Mar | Abr | Mai | Jun | Jul | Ago | Set | Out | Nov | Dez |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| 3 | 211 | 238 | 347 | 388 | 468 | 465 | 503 | 477 | 385 | 285 | 203 | 190 |
| 4 | 281 | 317 | 463 | 517 | 624 | 620 | 671 | 636 | 513 | 380 | 271 | 253 |
| 5 | 351 | 396 | 579 | 647 | 780 | 776 | 839 | 795 | 642 | 475 | 338 | 317 |
| 6 | 422 | 475 | 695 | 776 | 936 | 931 | 1 007 | 954 | 770 | 570 | 406 | 380 |
| 8 | 562 | 634 | 926 | 1 035 | 1 248 | 1 241 | 1 342 | 1 273 | 1 027 | 760 | 541 | 507 |

### Testes de sensibilidade

- **Mês usado como base do perfil de consumo (4 kWp):**
  - Só agosto: 518 €/ano, retorno de 12,5 anos.
  - Só setembro: 578 €/ano, retorno de 11,2 anos.
  - Os dois meses juntos: 546 €/ano, retorno de 11,9 anos.
- **Orientação:** todos os painéis a sul acrescentam cerca de 10–15 €/ano; todos a oeste perdem cerca de 25–30 €/ano.
- **Mais consumo no inverno:** num cenário ilustrativo em que o consumo de inverno, sem o carro, é até 35% maior, a poupança sobe ligeiramente (15–30 €/ano). Não é medido.

### Comparação com os dois meses medidos (4 kWp)

| | Agosto | Setembro | Total |
|---|---|---|---|
| Produção (PVGIS, mesmas datas) | cerca de 636 kWh | cerca de 513 kWh | cerca de 1 150 kWh (1 101–1 223) |
| Consumida diretamente | 273 kWh | 265 kWh | 538 kWh (47%) |
| Injetada na rede | 363 kWh | 248 kWh | 612 kWh |
| Parte do consumo coberta | 39% | 35% | 37% |

- Nestes dois meses, a fatura variável modelada teria sido de cerca de 147 € em vez de cerca de 239 € (cerca de 92 € menos), mais cerca de 18 € de venda de excedente a 0,03 €/kWh. A app mediu 237 € sem solar; a pequena diferença deve-se a a app aplicar o limite de IVA reduzido por ciclo de faturação e não por mês civil.

---

## 5. Baterias (modelado)

### Pressupostos

- 90% da capacidade nominal é utilizável; eficiência de ida e volta de 90%.
- Limites de potência: 2,5 kW para a bateria de 5 kWh e 5 kW para a de 10 kWh.
- A bateria só carrega com excedente solar.
- **Custo adicional (ilustrativo, segundo a 4gnews):** 5 kWh cerca de 3 200 € (2 000–4 200 €); 10 kWh cerca de 5 500 € (4 000–6 400 €).

### Resultados

| Acrescentada a 4 kWp | Energia solar extra consumida em casa, kWh/ano | Redução do excedente, kWh | Poupança extra | Retorno do custo extra: base (intervalo) | Preço máximo para retorno em 10 anos |
|---|---|---|---|---|---|
| 5 kWh | 1 403 | 1 559 | 192 €/ano | **17 anos** (10–22) | cerca de 1 900 € |
| 10 kWh | 2 091 | 2 324 | 286 €/ano | **19 anos** (14–22) | cerca de 2 900 € |

- Em sistemas maiores, o retorno da bateria é ligeiramente melhor (15–16 anos com 5–8 kWp). Só com as estimativas de custo mais baixas se aproxima dos 10 anos.
- **Cada kWh guardado só poupa cerca de 0,14 € líquidos:** evita cerca de 0,17 € de eletricidade da rede, mas perde 0,03 € de venda de excedente.
- **Passar o carregamento do carro para o dia faz parte do trabalho da bateria sem custo.** Com 50% do carregamento deslocado, uma bateria de 5 kWh só acrescenta cerca de 148 €/ano num sistema de 4 kWp.
- **No inverno há pouco excedente para guardar** (ver secção 3), por isso a bateria faria muito menos ciclos de novembro a fevereiro.

---

## 6. Consumos que podem passar para as horas de sol

- **Carregamento do carro elétrico (o único grande).** Em média 9,4 kWh/dia e 39% do consumo.
  - Passar 50% do carregamento diário para as horas de sol do mesmo dia acrescenta cerca de 140 €/ano com 4 kWp. Passar 100% (um limite máximo) acrescenta cerca de 215 €/ano.
  - Só funciona se o carro estiver em casa durante o dia, o que os dados não permitem confirmar.
  - O relé do carregador liga e desliga uma carga de cerca de 2,3 kW. Serve para uma automação do tipo "carregar quando há excedente solar", mas não consegue acompanhar o excedente de forma contínua. Com 4 kWp, a produção média ao meio-dia no verão é de cerca de 2–2,4 kW e a casa já gasta cerca de 1 kW, por isso o excedente fica normalmente abaixo dos 2,3 kW do carregador. Parte de cada carregamento solar continuaria a vir da rede; o modelo tem isso em conta.
- **Máquinas de lavar (cozinha e lavandaria).** Juntas gastam cerca de 18–20 kWh/mês à noite, o que vale no máximo cerca de 40 €/ano se passarem para o dia.
- **Ar condicionado do escritório.** Já é 79% diurno, e o arrefecimento no verão coincide naturalmente com o sol.
- **Não deslocáveis:** servidor/rede, frigorífico e forno.
- **Água quente e aquecimento: não identificáveis.** Nenhum equipamento os mede. Um termoacumulador elétrico, se existir, estará provavelmente nos 140–165 kWh/mês não abrangidos e seria um bom candidato a funcionar durante o dia.

---

## 7. Comparação financeira

### Pressupostos tarifários

- Tarifa configurada na app Shelly: 0,137378 €/kWh sem IVA, mais 0,001 €/kWh de imposto especial de consumo, com IVA a 6% nos primeiros 200 kWh por cada 30 dias e 23% no resto.
- Cada kWh que deixa de ser comprado à rede seria pago com IVA a 23%, por isso vale cerca de **0,170 €**.
- Os encargos fixos (potência contratada, contribuição audiovisual, taxa DGEG) ficam fora destes valores, porque não mudam com a instalação.

### Fatura e retorno, sem bateria

- Fatura variável anual sem solar: **cerca de 1 428 €** (modelado). Os 236,84 € medidos em 61 dias correspondem a cerca de 1 417 €/ano.
- Os preços de instalação incluem IVA a 23%. A taxa de 6% para equipamentos solares terminou a 30 de junho de 2025, e não foi encontrada nenhuma fonte que indique que o Orçamento do Estado para 2026 a tenha reposto.

| | 3 kWp | **4 kWp** | 5 kWp | 6 kWp* | 8 kWp* |
|---|---|---|---|---|---|
| Custo instalado: base (mín.–máx.) | 5 250 € (4,5–6,0 mil) | **6 500 €** (5,5–7,5 mil) | 7 750 € (6,5–9,0 mil) | 9 000 € (7,5–10,5 mil) | 11 500 € (9,5–13,5 mil) |
| Fatura após a instalação | 1 022 € | 967 € | 927 € | 897 € | 859 € |
| Venda de excedente a 0,03 €/kWh | 53 € | 85 € | 120 € | 156 € | 233 € |
| **Poupança anual líquida** | 460 € | **546 €** | 621 € | 687 € | 801 € |
| **Retorno simples** | 11,4 anos | **11,9** | 12,5 | 13,1 | 14,4 |
| Retorno com 50% do carregamento do carro nas horas de sol | 9,4 | **9,5** | 9,9 | 10,5 | 11,8 |
| Retorno com custo de instalação mínimo / máximo | 9,8 / 13,1 | 10,1 / 13,7 | 10,5 / 14,5 | 10,9 / 15,3 | 11,9 / 16,8 |
| Retorno sem venda de excedente | 12,9 | 14,1 | 15,5 | 17,0 | 20,2 |
| Retorno com venda a 0,05 €/kWh | 10,6 | 10,8 | 11,1 | 11,4 | 12,0 |
| Retorno com preços da rede −20% / +20% | 13,9 / 9,7 | 14,3 / 10,2 | 14,9 / 10,8 | 15,5 / 11,4 | 16,7 / 12,6 |

\*As fontes só cobrem 3–5 kWp, por isso os custos de 6 e 8 kWp são extrapolados. O custo foi modelado como cerca de 1 500 € fixos mais 1 250 € por kWp. Nenhum destes valores é um orçamento.

### Equipamento comprado pelo próprio (4 kWp)

| Item | Estimativa |
|---|---|
| 8 painéis de 500 W (Leroy Merlin, Hanersun TOPCon) | cerca de 616 € |
| Inversor de 4 kW com 2 entradas MPPT, híbrido (p. ex. Huawei SUN2000-4KTL-L1) | cerca de 700–900 € |
| 2 kits de fixação para telhado de telha, 4 painéis cada | cerca de 307 € |
| Ganchos de telha, cabos, proteções, contador (estimativa minha) | cerca de 300–500 € |
| Técnico responsável: ligação, termo de responsabilidade, registo na DGEG (estimativa minha) | cerca de 400–1 000 € |
| Entrega | cerca de 50–100 € |
| **Total** | **cerca de 2 400–3 400 €**, ou seja, retorno de cerca de **4,5–6 anos** com 546 €/ano |

- Acima de 800 W, a instalação tem de ser executada por uma entidade instaladora certificada ou por um técnico responsável, e registada na DGEG. **Encontre esse técnico antes de comprar o que quer que seja.**
- Fazer a instalação por conta própria tem riscos: trabalho no telhado, cablagem de corrente contínua de alta tensão, garantias dos painéis e do inversor e seguro da casa.

### O que o retorno não inclui

- Degradação dos painéis (cerca de 0,5%/ano).
- Inflação do preço da eletricidade.
- Substituição do inversor por volta do ano 10–15.
- Custo extra de contagem acima de 4 kW.

### Receber pela venda de excedente

- O excedente só é pago com um contrato com um comprador. Isso implica abrir atividade nas Finanças; os rendimentos até 1 000 €/ano estão isentos de IRS.
- Os preços publicados rondam 0,035–0,039 €/kWh fixos, ou indexados ao mercado. Sem contrato, a energia injetada não é paga.
- Com 4 kWp são cerca de **85–110 €/ano**, aproximadamente 15% da poupança. Mesmo sem venda de excedente, um sistema de 4 kWp paga-se em cerca de 14 anos.

### Verificar a elegibilidade para IVA reduzido

O IVA a 6% nos primeiros 200 kWh exige potência contratada igual ou inferior a 6,9 kVA, e o pico de 8,9 kW sugere que a sua pode ser superior. Isto altera a fatura antes do solar, mas não a poupança.

---

## 8. Incerteza

- **O consumo anual (cerca de 8 725 kWh/ano) é extrapolado a partir de dois meses de verão.** Continua a **não haver dados de inverno**.
  - O aquecimento no inverno (toalheiro elétrico, lareira, ar condicionado em modo bomba de calor) e as variações sazonais na quilometragem do carro são desconhecidos.
- **A distribuição do consumo ao longo do dia é um risco maior do que o total de kWh.** Se o consumo de inverno for sobretudo aquecimento ao fim do dia, o autoconsumo cai precisamente quando a produção é mais baixa. Isso favorece ficar nos 4 kWp ou menos.
- **Agosto inclui dias de férias.** Mostra como o resultado depende da ocupação da casa: o teletrabalho (setembro) aumenta o consumo diurno e melhora o valor do solar.
- **Cerca de 4,7% da distribuição horária do período foi reconstruída**, sobretudo em setembro (6, 9–10, 12 e 30 de setembro).
- **Os passos de 15 minutos sobrestimam o autoconsumo em alguns pontos percentuais.** Picos curtos não podem ser acompanhados com exatidão, e a produção é distribuída de forma uniforme dentro de cada hora.
- **Sombreamento:** o relevo foi verificado e é desprezável. **Os obstáculos próximos** (árvores, prédios, chaminés) **continuam desconhecidos** e são os que mais contam no inverno.
- **A orientação, a inclinação e a área útil do telhado continuam a ser pressupostos** (exatamente sul e oeste, 25°).
- **Os custos e os preços de venda de excedente são indicações de mercado, não orçamentos.**

---

## 9. Próximos passos

### Dados

1. **Aumentar já a retenção de dados ao minuto da Shelly para pelo menos 400 dias.** Os dados de 15 minutos com mais de 90 dias estão a ser apagados, e o início de agosto começa a desaparecer por volta de 30 de outubro de 2026.
2. **Obter 12 ou mais meses de leituras do contador inteligente da E-REDES** (dados de 15 minutos, no Balcão Digital da E-REDES). É o dado mais valioso, porque acrescenta o inverno.
3. **Repetir esta análise em fevereiro–março de 2027**, com os dados Shelly de outubro a janeiro.
4. **Fatura da eletricidade:** potência contratada, tipo de tarifa, e se uma tarifa bi-horária seria mais barata para carregar o carro à noite.

### O telhado (as coordenadas exatas já estão feitas)

5. **Orientação exata de cada água, em graus.** Meça no Google Maps a direção da beira de cada água, ou use a bússola do telemóvel encostado à beira, de frente para fora.
6. **Inclinação do telhado.** Uma app de nível no telemóvel, pousada no telhado ou no forro do sótão, chega.
7. **Medidas de cada água** (comprimento × largura), para ver quantos painéis cabem em cada uma.
8. **Obstáculos próximos:** direção, altura e distância de árvores, prédios e chaminés, sobretudo a sudeste, sul e sudoeste. O ideal são fotografias tiradas do telhado nessas direções.

### A casa

9. **Disponibilidade do carro e carregador:** quantos dias úteis o carro fica em casa durante o dia, e se vale a pena um carregador que acompanhe o excedente solar.
10. **Água quente e aquecimento:** que energia usam o termoacumulador e o aquecimento, e quanto consomem.
11. **Identificar o consumo "Garage Tests"** (cerca de 59 W constantes, cerca de 520 kWh/ano). Se não for necessário, desligá-lo poupa cerca de 90 €/ano sem qualquer investimento.

### Instaladores e compradores

12. **Visita técnica de um instalador** para confirmar os pontos 5 a 8.
13. **Orçamentos para cerca de 4 kWp,** tanto chave na mão como só com instalação de equipamento fornecido por si. Pergunte se o inversor fica em 4 kW ou menos, qual o preço por kWp para 5 kWp, e o preço da bateria em linha separada.
14. **Comparar ofertas de compra de excedente** com a ferramenta de comparação da ERSE, antes de decidir se vale a pena registar-se para vender.

---

## Fontes

- Produção solar, horizonte do relevo e inclinação ótima: [PVGIS v5.3 (JRC, Comissão Europeia)](https://re.jrc.ec.europa.eu/pvg_tools/en/), via API: dados SARAH3/ERA5, 38,950780 N, −9,037919 E, montagem em telhado, 14% de perdas
- Posição do sol: equações de posição solar da NOAA, calculadas para as coordenadas exatas
- Custos de instalação e baterias: [4gnews – custos 2026](https://4gnews.pt/vais-instalar-paineis-solares-fizemos-as-contas-antes-de-gastares-dinheiro/), [4gnews – baterias](https://4gnews.pt/isto-pode-baixar-a-tua-conta-da-luz-mas-sera-que-compensa/), [E-konomista](https://www.e-konomista.pt/vale-sempre-a-pena-instalar-paineis-solares-em-casas-particulares/)
- Preços de equipamento: [Leroy Merlin – Hanersun 500 W](https://www.leroymerlin.pt/produtos/painel-solar-hanersun-topcon-500-w-95653745.html), [Tienda Solar – Huawei SUN2000-4KTL-L1](https://tienda-solar.es/en/inverters/976-solar-inverter-huawei-sun2000-4ktl-l1), [FV Componentes](https://fvcomponentes.com/en/?p=3175), [Sun Suprimentos – kits de fixação](https://suministrosdelsol.com/pt/tampa-inclinada/1393-11133-suporte-coplanar-para-4-paineis-tampa-metalica-ou-azulejo-vertical-ate-2400x1150.html)
- IVA dos painéis solares de volta aos 23%: [The Portugal News](https://www.theportugalnews.com/pt/noticias/2026-07-13/a-taxa-de-iva-de-6-para-a-energia-solar-em-portugal-vai-expirar-e-voltara-a-23-em-julho-de-2025/133871), [Jornal de Negócios](https://www.jornaldenegocios.pt/empresas/energia/detalhe/associacoes-apelam-para-descida-do-iva-nos-paineis-solares-e-ar-condicionado)
- Regras do autoconsumo, obrigatoriedade de instalador certificado e venda de excedente: [Doutor Finanças](https://www.doutorfinancas.pt/?p=213476), [apresentação da ADENE](https://www.cm-maia.pt/cmmaia/uploads/writer_file/document/13010/apresentacao_maia_empoweringus_17072025_adene.pdf), [Universidade de Évora](https://www.catedraer.uevora.pt/content/download/392/2196/version/1/file/Autoconsumo+-+DecLei162-2019+v1.2.pdf)
- Preços de venda de excedente: [DECO Proteste](https://www.deco.proteste.pt/comunidades/energias-renovaveis/produzir-eletricidade/conversation/5185/preco-da-compensacao-por-excedente), [Jornal Económico](https://jornaleconomico.sapo.pt/?p=23546), [EC Clean Energy Islands](https://clean-energy-islands.ec.europa.eu/countries/portugal/legal/res-electricity/net-metering-net-billing)
- IVA reduzido na eletricidade: [Repsol](https://www.repsol.pt/particulares/assessoramento/iva-eletricidade-tudo-que-precisa-saber/)
- Tarifa: a configurada na app Shelly (README da exportação)
