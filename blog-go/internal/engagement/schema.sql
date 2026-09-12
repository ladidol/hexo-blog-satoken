CREATE TABLE tb_like (
  user_info_id INTEGER NOT NULL REFERENCES tb_user_info(id) ON DELETE CASCADE,
  target_type INTEGER NOT NULL CHECK(target_type IN (1,2,3)),
  target_id INTEGER NOT NULL, create_time TEXT NOT NULL,
  PRIMARY KEY(user_info_id,target_type,target_id)
);
CREATE INDEX idx_like_target ON tb_like(target_type,target_id);

CREATE TABLE tb_article_view (
  article_id INTEGER NOT NULL REFERENCES tb_article(id) ON DELETE CASCADE,
  visitor_key TEXT NOT NULL, create_time TEXT NOT NULL,
  PRIMARY KEY(article_id,visitor_key)
);
