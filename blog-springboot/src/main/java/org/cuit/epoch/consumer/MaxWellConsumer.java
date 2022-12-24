package org.cuit.epoch.consumer;

import com.alibaba.fastjson.JSON;
import org.cuit.epoch.dto.article.ArticleSearchDTO;
import org.cuit.epoch.dto.article.MaxwellDataDTO;
import org.cuit.epoch.entity.Article;
import org.cuit.epoch.mapper.ArticleSearchRepository;
import org.cuit.epoch.util.BeanCopyUtils;
import org.springframework.amqp.rabbit.annotation.RabbitHandler;
import org.springframework.amqp.rabbit.annotation.RabbitListener;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Component;

import static org.cuit.epoch.constant.MQPrefixConst.MAXWELL_QUEUE;

/**
 * @author: Xiaoqiang-Ladidol
 * @date: 2022/12/22 2:04
 * @description: {maxwell监听数据}
 */
@Component
@RabbitListener(queues = MAXWELL_QUEUE)
public class MaxWellConsumer {
    @Autowired
    private ArticleSearchRepository elasticsearchRepository;

    @RabbitHandler
    public void process(byte[] data) {
        // 获取监听信息
        MaxwellDataDTO maxwellDataDTO = JSON.parseObject(new String(data), MaxwellDataDTO.class);
        // 获取文章数据
        Article article = JSON.parseObject(JSON.toJSONString(maxwellDataDTO.getData()), Article.class);
        // 判断操作类型
        switch (maxwellDataDTO.getType()) {
            case "insert":
            case "update":
                // 更新es文章
                elasticsearchRepository.save(BeanCopyUtils.copyObject(article, ArticleSearchDTO.class));
                break;
            case "delete":
                // 删除文章
                elasticsearchRepository.deleteById(article.getId());
                break;
            default:
                break;
        }
    }

}