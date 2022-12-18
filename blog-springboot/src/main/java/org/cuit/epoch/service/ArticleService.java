package org.cuit.epoch.service;

import com.baomidou.mybatisplus.extension.service.IService;
import org.cuit.epoch.dto.article.*;
import org.cuit.epoch.dto.article.ArticleHomeDTO;
import org.cuit.epoch.entity.Article;
import org.cuit.epoch.vo.ArticleTopVO;
import org.cuit.epoch.vo.ArticleVO;
import org.cuit.epoch.vo.ConditionVO;
import org.cuit.epoch.vo.DeleteVO;
import org.cuit.epoch.vo.page.PageResult;

import java.util.List;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/17 22:06
 * @description: {}
 */
public interface ArticleService extends IService<Article> {

    /**
     * 查询文章归档
     *
     * @return 文章归档
     */
    PageResult<ArchiveDTO> listArchives();

    /**
     * 查询后台文章
     *
     * @param condition 条件
     * @return 文章列表
     */
    PageResult<ArticleBackDTO> listArticleBacks(ConditionVO condition);

    /**
     * 查询首页文章
     *
     * @return 文章列表
     */
    List<ArticleHomeDTO> listArticles();

    /**
     * 根据条件查询文章列表
     *
     * @param condition 条件
     * @return 文章列表
     */
    ArticlePreviewListDTO listArticlesByCondition(ConditionVO condition);

    /**
     * 搜索文章
     *
     * @param condition 条件
     * @return 文章列表
     */
//    List<ArticleSearchDTO> listArticlesBySearch(ConditionVO condition);

    /**
     * 根据id查看后台文章
     *
     * @param articleId 文章id
     * @return 文章列表
     */
    ArticleVO getArticleBackById(Integer articleId);

    /**
     * 根据id查看文章
     *
     * @param articleId 文章id
     * @return {@link ArticleDTO} 文章信息
     */
    ArticleDTO getArticleById(Integer articleId);

    /**
     * 点赞文章
     *
     * @param articleId 文章id
     */
    void saveArticleLike(Integer articleId);

    /**
     * 添加或修改文章
     *
     * @param articleVO 文章信息
     */
    void saveOrUpdateArticle(ArticleVO articleVO);

    /**
     * 修改文章置顶
     *
     * @param articleTopVO 文章置顶信息
     */
    void updateArticleTop(ArticleTopVO articleTopVO);

    /**
     * 删除或恢复文章
     *
     * @param deleteVO 逻辑删除对象
     */
    void updateArticleDelete(DeleteVO deleteVO);

    /**
     * 物理删除文章
     *
     * @param articleIdList 文章id集合
     */
    void deleteArticles(List<Integer> articleIdList);

    /**
     * 导出文章
     *
     * @param articleIdList 文章id列表
     * @return {@link List}<{@link String}> 文件地址
     */
    List<String> exportArticles(List<Integer> articleIdList);
}
