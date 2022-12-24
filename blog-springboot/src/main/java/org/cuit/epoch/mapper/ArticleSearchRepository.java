package org.cuit.epoch.mapper;

import org.cuit.epoch.dto.article.ArticleSearchDTO;
import org.springframework.data.elasticsearch.repository.ElasticsearchRepository;
import org.springframework.stereotype.Repository;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/22 1:49
 * @description: {}
 */
@Repository
public interface ArticleSearchRepository extends ElasticsearchRepository<ArticleSearchDTO,Integer> {
}
