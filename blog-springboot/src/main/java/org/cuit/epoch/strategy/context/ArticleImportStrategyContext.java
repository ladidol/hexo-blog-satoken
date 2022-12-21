package org.cuit.epoch.strategy.context;

import org.cuit.epoch.enums.MarkdownTypeEnum;
import org.cuit.epoch.strategy.ArticleImportStrategy;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.util.Map;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/21 18:35
 * @description: {文章导入策略上下文}
 */
@Service
public class ArticleImportStrategyContext {
    @Autowired
    private Map<String, ArticleImportStrategy> articleImportStrategyMap;

    public void importArticles(MultipartFile file, String type) {
        articleImportStrategyMap.get(MarkdownTypeEnum.getMarkdownType(type)).importArticles(file);
    }
}
