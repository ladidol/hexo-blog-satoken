CREATE TABLE tb_article (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  category_id INTEGER NULL DEFAULT NULL,
  article_cover TEXT NULL DEFAULT NULL,
  article_title TEXT NOT NULL,
  article_content TEXT NOT NULL,
  type INTEGER NOT NULL DEFAULT 0,
  original_url TEXT NULL DEFAULT NULL,
  is_top INTEGER NOT NULL DEFAULT 0,
  is_delete INTEGER NOT NULL DEFAULT 0,
  status INTEGER NOT NULL DEFAULT 1,
  create_time TEXT NOT NULL,
  update_time TEXT NULL DEFAULT NULL,
  FOREIGN KEY (user_id) REFERENCES tb_user_info(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (category_id) REFERENCES tb_category(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX idx_article_public ON tb_article(status,is_delete,is_top DESC,create_time DESC,id DESC);
CREATE INDEX idx_article_category ON tb_article(category_id,status,is_delete);

CREATE TABLE tb_article_tag (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  article_id INTEGER NOT NULL,
  tag_id INTEGER NOT NULL,
  FOREIGN KEY (article_id) REFERENCES tb_article(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
  FOREIGN KEY (tag_id) REFERENCES tb_tag(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
  UNIQUE (article_id,tag_id)
);
CREATE INDEX idx_article_tag_tag ON tb_article_tag(tag_id,article_id);
