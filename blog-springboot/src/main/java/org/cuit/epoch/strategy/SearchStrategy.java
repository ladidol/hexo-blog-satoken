package org.cuit.epoch.strategy;

import org.cuit.epoch.dto.article.ArticleSearchDTO;

import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/21 22:07
 * @description: {搜索策略}
 */
public interface SearchStrategy {

    /**
     * 搜索文章
     *
     * @param keywords 关键字
     * @return {@link List <ArticleSearchDTO>} 文章列表
     */
    List<ArticleSearchDTO> searchArticle(String keywords);

}