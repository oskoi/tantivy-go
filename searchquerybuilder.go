package tantivy_go

type QueryType int

const (
	BoolQuery QueryType = iota
	PhraseQuery
	PhrasePrefixQuery
	TermPrefixQuery
	TermQuery
	EveryTermQuery
	OneOfTermQuery
	AllQuery
	// RangeQuery
	// FuzzyQuery
)

type QueryModifier int

const (
	Must QueryModifier = iota
	Should
	MustNot
)

type FieldQuery struct {
	FieldIndex int     `json:"field_index"`
	TextIndex  int     `json:"text_index"`
	Boost      float64 `json:"boost"`
}

type QueryElement struct {
	Query     Query         `json:"query"`
	Modifier  QueryModifier `json:"query_modifier"`
	QueryType QueryType     `json:"query_type"`
}

type BooleanQuery struct {
	Subqueries []QueryElement `json:"subqueries"`
	Boost      float64        `json:"boost"`
}

type FinalQuery struct {
	Query  *BooleanQuery `json:"query"`
	Fields []string      `json:"fields"`
	Texts  []string      `json:"texts"`
}

type sharedStore struct {
	fields []string
	texts  []string
}

type QueryBuilder struct {
	store      *sharedStore
	subqueries []QueryElement
}

func NewQueryBuilder() *QueryBuilder {
	return &QueryBuilder{
		store: &sharedStore{
			fields: make([]string, 0),
			texts:  make([]string, 0),
		},
		subqueries: make([]QueryElement, 0),
	}
}

func (qb *QueryBuilder) NestedBuilder() *QueryBuilder {
	return &QueryBuilder{
		store:      qb.store,
		subqueries: make([]QueryElement, 0),
	}
}

func (qb *QueryBuilder) AddText(text string) int {
	for i, t := range qb.store.texts {
		if t == text {
			return i
		}
	}
	qb.store.texts = append(qb.store.texts, text)
	return len(qb.store.texts) - 1
}

func (qb *QueryBuilder) AddField(field string) int {
	for i, f := range qb.store.fields {
		if f == field {
			return i
		}
	}
	qb.store.fields = append(qb.store.fields, field)
	return len(qb.store.fields) - 1
}

func (qb *QueryBuilder) Query(modifier QueryModifier, field string, text string, queryType QueryType, boost float64) *QueryBuilder {
	fieldIndex := qb.AddField(field)
	textIndex := qb.AddText(text)
	qb.subqueries = append(qb.subqueries, QueryElement{
		Query: &FieldQuery{
			FieldIndex: fieldIndex,
			TextIndex:  textIndex,
			Boost:      boost,
		},
		Modifier:  modifier,
		QueryType: queryType,
	})
	return qb
}

func (qb *QueryBuilder) BooleanQuery(modifier QueryModifier, subBuilder *QueryBuilder, boost float64) *QueryBuilder {
	if qb == nil || subBuilder == nil || qb.store != subBuilder.store {
		panic("nested query builder must be created with parent.NestedBuilder()")
	}
	qb.subqueries = append(qb.subqueries, QueryElement{
		Query: &BooleanQuery{
			Subqueries: subBuilder.subqueries,
			Boost:      boost,
		},
		Modifier:  modifier,
		QueryType: BoolQuery,
	})
	return qb
}

func (qb *QueryBuilder) AllQuery(modifier QueryModifier, boost float64) *QueryBuilder {
	qb.subqueries = append(qb.subqueries, QueryElement{
		Query: &AllQueryStruct{
			Boost: boost,
		},
		Modifier:  modifier,
		QueryType: AllQuery,
	})
	return qb
}

func (qb *QueryBuilder) Build() FinalQuery {
	return FinalQuery{
		Query: &BooleanQuery{
			Subqueries: qb.subqueries,
			Boost:      0,
		},
		Fields: qb.store.fields,
		Texts:  qb.store.texts,
	}
}

type Query interface {
	IsQuery()
}

func (fq *FieldQuery) IsQuery() {}

func (bq *BooleanQuery) IsQuery() {}

type AllQueryStruct struct {
	Boost float64 `json:"boost"`
}

func (aq *AllQueryStruct) IsQuery() {}

// Note: RangeQuery and FuzzyQuery are not yet implemented
// They require tantivy API changes
//
// type RangeQueryStruct struct {
// 	FieldIndex   int       `json:"field_index"`
// 	LowerBound   string    `json:"lower_bound"`
// 	UpperBound   string    `json:"upper_bound"`
// 	IncludeLower bool      `json:"include_lower"`
// 	IncludeUpper bool      `json:"include_upper"`
// 	RangeType    int       `json:"range_type"`
// 	Boost        float64   `json:"boost"`
// }
//
// func (qb *QueryBuilder) RangeQuery(modifier QueryModifier, field string, lowerBound, upperBound string, includeLower, includeUpper bool, rangeType int, boost float64) *QueryBuilder {
// 	// Not yet implemented
// 	return qb
// }
//
// type FuzzyQueryStruct struct {
// 	FieldIndex   int     `json:"field_index"`
// 	TextIndex    int     `json:"text_index"`
// 	Distance     uint8   `json:"distance"`
// 	PrefixLength uint    `json:"prefix_length"`
// 	Boost        float64 `json:"boost"`
// }
//
// func (qb *QueryBuilder) FuzzyQuery(modifier QueryModifier, field string, text string, distance uint8, prefixLength uint, boost float64) *QueryBuilder {
// 	// Not yet implemented
// 	return qb
// }
