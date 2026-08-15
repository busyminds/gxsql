-- gxsql query-cost baseline fixture.
-- Seed: execute this file once in an empty database.
-- Rows: 8 total; one empty name; one duplicate id; four repeated values.
-- Indexes: primary key on row_id and value index for representative lookup cost.
CREATE TABLE gxsql_bench_users (
    row_id INTEGER NOT NULL PRIMARY KEY,
    id INTEGER NOT NULL,
    name TEXT NOT NULL,
    value INTEGER NOT NULL
);

INSERT INTO gxsql_bench_users (row_id, id, name, value) VALUES
    (1, 100, 'alpha', 10),
    (2, 100, 'beta', 10),
    (3, 101, '', 20),
    (4, 102, 'delta', 20),
    (5, 103, 'epsilon', 30),
    (6, 104, 'zeta', 30),
    (7, 105, 'eta', 40),
    (8, 106, 'theta', 40);

CREATE INDEX gxsql_bench_users_value_idx ON gxsql_bench_users (value);
