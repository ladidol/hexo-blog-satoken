package org.cuit.epoch.strategy.context;

import org.cuit.epoch.dto.article.ArticleSearchDTO;
import org.cuit.epoch.strategy.SearchStrategy;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;

import java.util.List;
import java.util.Map;

import static org.cuit.epoch.enums.SearchModeEnum.getStrategy;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/21 22:00
 * @description: {搜索策略上下文}
 */
@Service
public class SearchStrategyContext {
    /**
     * 搜索模式
     */
    @Value("${search.mode}")
    private String searchMode;

    @Autowired
    private Map<String, SearchStrategy> searchStrategyMap;

    /**
     * 执行搜索策略
     *
     * @param keywords 关键字
     * @return {@link List < ArticleSearchDTO >} 搜索文章
     */
    public List<ArticleSearchDTO> executeSearchStrategy(String keywords) {
        return  searchStrategyMap.get(getStrategy(searchMode)).searchArticle(keywords);
    }

}
