package org.cuit.epoch.mapper;

import com.baomidou.mybatisplus.core.mapper.BaseMapper;
import org.apache.ibatis.annotations.Param;
import org.cuit.epoch.dto.article.*;
import org.cuit.epoch.dto.article.ArticleHomeDTO;
import org.cuit.epoch.entity.Article;
import org.cuit.epoch.vo.ConditionVO;
import org.springframework.stereotype.Repository;

import java.util.List;

/**
 * @author: ladidol
 * @date: 2022/11/16 19:24
 * @description:
 */
@Repository
public interface ArticleMapper extends BaseMapper<Article> {

    /**
     * 查询首页文章
     *
     * @param current 页码
     * @param size    大小
     * @return 文章列表
     */
    List<ArticleHomeDTO> listArticles(@Param("current") Long current, @Param("size") Long size);

    /**
     * 根据id查询文章
     *
     * @param articleId 文章id
     * @return 文章信息
     */
    ArticleDTO getArticleById(@Param("articleId") Integer articleId);

    /**
     * 根据条件查询文章
     *
     * @param current   页码
     * @param size      大小
     * @param condition 条件
     * @return 文章列表
     */
    List<ArticlePreviewDTO> listArticlesByCondition(@Param("current") Long current, @Param("size") Long size, @Param("condition") ConditionVO condition);

    /**
     * 查询后台文章
     *
     * @param current   页码
     * @param size      大小
     * @param condition 条件
     * @return 文章列表
     */
    List<ArticleBackDTO> listArticleBacks(@Param("current") Long current, @Param("size") Long size, @Param("condition") ConditionVO condition);

    /**
     * 查询后台文章总量
     *
     * @param condition 条件
     * @return 文章总量
     */
    Integer countArticleBacks(@Param("condition") ConditionVO condition);

    /**
     * 查看文章的推荐文章
     *
     * @param articleId 文章id
     * @return 文章列表
     */
    List<ArticleRecommendDTO> listRecommendArticles(@Param("articleId") Integer articleId);

    /**
     * 文章统计
     *
     * @return {@link List<ArticleStatisticsDTO>} 文章统计结果
     */
    List<ArticleStatisticsDTO> listArticleStatistics();

}