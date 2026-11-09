-- An absent DOI or PMID is NULL, so UNIQUE holds only between ids present.
CREATE TABLE publications (
    id     text  PRIMARY KEY,
    doi    text  UNIQUE,
    pmid   text  UNIQUE,
    record jsonb NOT NULL,
    CHECK (doi IS NOT NULL OR pmid IS NOT NULL)
);

CREATE TABLE papers (
    topic_id       text  NOT NULL REFERENCES topics,
    id             text  NOT NULL,
    publication_id text  NOT NULL REFERENCES publications,
    record         jsonb NOT NULL,
    PRIMARY KEY (topic_id, id),
    UNIQUE (topic_id, publication_id)
);
